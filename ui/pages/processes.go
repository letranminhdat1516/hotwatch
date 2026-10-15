package pages

import (
	"cmp"
	"fmt"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// procCol describes one column of the process table.
type procCol struct {
	id, title string
	width     float32
	min       float32
	align     ui.Align
}

// procCols in their default order: the load figures first, identity after.
var procCols = []procCol{
	{id: "name", title: "Name", min: 100},
	{id: "cpu", title: "CPU", width: 72, min: 50, align: ui.End},
	{id: "mem", title: "Memory", width: 84, min: 60, align: ui.End},
	{id: "threads", title: "Threads", width: 80, min: 72, align: ui.End},
	{id: "pid", title: "PID", width: 64, min: 44, align: ui.End},
	{id: "user", title: "User", width: 90, min: 50},
	{id: "ports", title: "Ports", width: 110, min: 60},
	{id: "started", title: "Started", width: 104, min: 80},
}

func colByID(id string) (procCol, bool) {
	for _, c := range procCols {
		if c.id == id {
			return c, true
		}
	}
	return procCol{}, false
}

func allColumns() []string {
	out := make([]string, len(procCols))
	for i, c := range procCols {
		out[i] = c.id
	}
	return out
}

// procKey identifies a process: a PID alone is reused, with its start time
// it is not.
type procKey struct {
	pid   int32
	start int64
}

func startNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func keyOf(p *model.Process) procKey { return procKey{p.PID, startNano(p.StartTime)} }

// ProcessesInput is what the Processes page reads each frame.
type ProcessesInput struct {
	HostID string
	View   hub.HostView
	// Gen changes whenever View does; sorting and filtering rerun only then,
	// or when the sort, the search or the tree mode change.
	Gen     uint64
	Now     time.Time
	Width   float32 // room for the page; 0: the window's width
	Actions *Actions
}

// Processes is the Processes page and its state. Main thread only.
type Processes struct {
	// OnColumns is called with the visible column ids in order whenever the
	// user hides, shows or moves a column.
	OnColumns func(cols []string)

	search ui.Handle // the search field

	cols   []string
	layout ui.TableLayout
	sort   ui.SortOrder

	query     string
	scope     int // which processes to list: an index into scopeLabels
	treeMode  bool
	collapsed map[procKey]bool // tree items the user closed; the rest are open

	list    ui.ListState
	tree    ui.OutlineState[procKey]
	row     int
	sel     procKey
	hasSel  bool
	host    string
	cache   procCache
	split   float32
	inspW   float32
	lastW   float32
	toggled string

	askTerm, askKill1, askKill2 bool
	pending                     pendingKill
}

type pendingKill struct {
	host, name string
	pid        int32
	nano       int64
}

// NewProcesses returns the page with every column, sorted by CPU, largest first.
func NewProcesses() *Processes {
	return &Processes{cols: allColumns(), sort: ui.SortOrder{Column: "cpu", Descending: true}, inspW: 320, row: -1}
}

// SetColumns restores the saved visible columns and their order. Unknown and
// repeated ids are dropped; nothing left means every column.
func (p *Processes) SetColumns(ids []string) {
	var cols []string
	for _, id := range ids {
		if _, ok := colByID(id); ok && !slices.Contains(cols, id) {
			cols = append(cols, id)
		}
	}
	if len(cols) == 0 {
		cols = allColumns()
	}
	p.cols = cols
	p.layout.Order = slices.Clone(cols)
}

func (p *Processes) setColumns(cols []string) {
	p.cols = cols
	p.layout.Order = slices.Clone(cols)
	if p.OnColumns != nil {
		p.OnColumns(slices.Clone(cols))
	}
}

// procCache is the sorted, filtered rows, valid until its key changes.
type procCache struct {
	valid bool
	key   procCacheKey

	keys  []procKey // per index into the process list
	index map[procKey]int
	rows  []int     // list mode: indices, in order
	roots []procKey // tree mode
	kids  map[procKey][]procKey
	shown int // processes passing the search
}

type procCacheKey struct {
	host  string
	gen   uint64
	n     int
	sort  ui.SortOrder
	query string
	tree  bool
	scope int
}

func (p *Processes) ensure(in ProcessesInput) {
	key := procCacheKey{in.HostID, in.Gen, len(in.View.Procs), p.sort, p.query, p.treeMode, p.scope}
	if p.cache.valid && p.cache.key == key {
		return
	}
	ps := in.View.Procs
	ch := procCache{valid: true, key: key, keys: make([]procKey, len(ps)), index: make(map[procKey]int, len(ps))}
	for i := range ps {
		ch.keys[i] = keyOf(&ps[i])
		ch.index[ch.keys[i]] = i
	}
	f := parseFilter(p.query)
	keep := make([]bool, len(ps))
	for i := range ps {
		if keep[i] = f.match(&ps[i]) && inScope(&ps[i], p.scope); keep[i] {
			ch.shown++
		}
	}
	less := func(a, b int) bool { return lessProc(&ps[a], &ps[b], p.sort) }
	if p.treeMode {
		tree := buildTree(ps, keep, less)
		for _, r := range tree.Roots {
			ch.roots = append(ch.roots, ch.keys[r])
		}
		ch.kids = make(map[procKey][]procKey, len(tree.Children))
		for parent, kids := range tree.Children {
			ks := make([]procKey, len(kids))
			for i, k := range kids {
				ks[i] = ch.keys[k]
			}
			ch.kids[ch.keys[parent]] = ks
		}
	} else {
		for i := range ps {
			if keep[i] {
				ch.rows = append(ch.rows, i)
			}
		}
		slices.SortStableFunc(ch.rows, func(a, b int) int {
			switch {
			case less(a, b):
				return -1
			case less(b, a):
				return 1
			}
			return 0
		})
	}
	p.cache = ch
}

// procFilter is a parsed search: plain words match name, command and user
// as substrings; port:, user: and pid: tokens match exactly. All must hold.
type procFilter struct {
	words []string
	ports []uint16
	users []string
	pids  []int32
}

func parseFilter(q string) procFilter {
	var f procFilter
	for _, tok := range strings.Fields(q) {
		kind, val, ok := strings.Cut(tok, ":")
		switch {
		case ok && strings.EqualFold(kind, "port"):
			if n, err := strconv.ParseUint(val, 10, 16); err == nil {
				f.ports = append(f.ports, uint16(n))
			} else if val != "" {
				f.words = append(f.words, strings.ToLower(tok))
			}
		case ok && strings.EqualFold(kind, "pid"):
			if n, err := strconv.ParseInt(val, 10, 32); err == nil {
				f.pids = append(f.pids, int32(n))
			} else if val != "" {
				f.words = append(f.words, strings.ToLower(tok))
			}
		case ok && strings.EqualFold(kind, "user"):
			if val != "" {
				f.users = append(f.users, val)
			}
		default:
			f.words = append(f.words, strings.ToLower(tok))
		}
	}
	return f
}

func (f procFilter) match(p *model.Process) bool {
	for _, n := range f.ports {
		if !slices.ContainsFunc(p.Ports, func(q model.Port) bool { return q.Number == n }) {
			return false
		}
	}
	for _, n := range f.pids {
		if p.PID != n {
			return false
		}
	}
	for _, u := range f.users {
		if p.User != u {
			return false
		}
	}
	if len(f.words) == 0 {
		return true
	}
	name, cmd, user := strings.ToLower(p.Name), strings.ToLower(p.Command), strings.ToLower(p.User)
	for _, w := range f.words {
		if !strings.Contains(name, w) && !strings.Contains(cmd, w) && !strings.Contains(user, w) {
			return false
		}
	}
	return true
}

// sortValue is a process's value in column id; known is false where the
// value is unreadable or absent, which sorts after every known one.
func sortValue(p *model.Process, id string) (num float64, str string, known bool) {
	switch id {
	case "name":
		return 0, strings.ToLower(p.Name), p.Name != ""
	case "user":
		return 0, strings.ToLower(p.User), p.User != ""
	case "pid":
		return float64(p.PID), "", true
	case "cpu":
		return p.CPUPct, "", true
	case "mem":
		return float64(p.RSS), "", p.RSS > 0 // 0: unreadable
	case "threads":
		return float64(p.Threads), "", p.Threads > 0
	case "ports":
		if len(p.Ports) == 0 {
			return 0, "", false
		}
		low := p.Ports[0].Number
		for _, q := range p.Ports {
			low = min(low, q.Number)
		}
		return float64(low), "", true
	case "started":
		return float64(startNano(p.StartTime)), "", !p.StartTime.IsZero()
	}
	return 0, "", false
}

func lessProc(a, b *model.Process, s ui.SortOrder) bool {
	an, as, ak := sortValue(a, s.Column)
	bn, bs, bk := sortValue(b, s.Column)
	if ak != bk {
		return ak // unknown last, in either direction
	}
	if ak {
		c := cmp.Or(cmp.Compare(an, bn), strings.Compare(as, bs))
		if s.Descending {
			c = -c
		}
		if c != 0 {
			return c < 0
		}
	}
	return a.PID < b.PID
}

func (p *Processes) sync(in ProcessesInput) {
	if p.host != in.HostID {
		p.host = in.HostID
		p.hasSel, p.row = false, -1
		p.cache.valid = false
		p.askTerm, p.askKill1, p.askKill2 = false, false, false
	}
}

// FocusSearch gives the search field the keyboard focus.
func (p *Processes) FocusSearch() { p.search.Focus() }

// Render builds the page.
func (p *Processes) Render(c *ui.Context, in ProcessesInput) {
	p.sync(in)
	ps := in.View.Procs
	ui.Column(c).Gap(2 * g()).Children(func() {
		if err := in.View.ProcsErr; err != nil {
			components.Banner(c, components.BannerWarning, "Process list may be stale: "+err.Error())
		}
		ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Children(func() {
			ui.SearchField(c, &p.query).Label("Search").Bind(&p.search).Grow(1)
			ui.Segmented(c, &p.scope, scopeLabels...)
			ui.Checkbox(c, &p.treeMode, "Tree view")
		})
		p.ensure(in)
		p.actionLine(c, in)
		if len(ps) == 0 {
			components.EmptyState(c, "Waiting for the process list…")
			return
		}
		p.content(c, in)
		p.footer(c, in)
	})
	p.dialogs(c, in)
}

func (p *Processes) actionLine(c *ui.Context, in ProcessesInput) {
	a, ok := in.Actions.Last(in.HostID, "signal")
	if !ok {
		return
	}
	verb := "Terminate"
	if a.Verb == string(model.SigKill) {
		verb = "Force kill"
	}
	who := a.Target
	if pidS, startS, ok := strings.Cut(a.Target, "@"); ok {
		pid, err1 := strconv.ParseInt(pidS, 10, 32)
		nano, err2 := strconv.ParseInt(startS, 10, 64)
		who = pidS
		if err1 == nil && err2 == nil {
			if i, ok := p.cache.index[procKey{int32(pid), nano}]; ok && i < len(in.View.Procs) {
				who = fmt.Sprintf("%s (%d)", in.View.Procs[i].Name, pid)
			}
		}
	}
	text := fmt.Sprintf("%s %s: %s", verb, who, a.State)
	t := c.Theme()
	col := t.TextMuted
	if a.State == "failed" {
		col = t.Danger
		if a.Err != nil {
			text += ": " + a.Err.Message
		}
	}
	ui.Text(c, text).FontSize(13).TextColor(col)
}

func (p *Processes) content(c *ui.Context, in ProcessesInput) {
	ps := in.View.Procs
	_, wh := c.Size()
	h := max(wh-300, 240)
	sel := -1 // index of the chosen process in ps
	if p.hasSel {
		if i, ok := p.cache.index[p.sel]; ok && i < len(ps) && p.visible(i) {
			sel = i
		} else {
			p.hasSel = false
		}
	}
	if sel < 0 {
		p.table(c, in).Height(h)
		return
	}
	width := in.Width
	if width <= 0 {
		width, _ = c.Size()
	}
	if width != p.lastW {
		p.split, p.lastW = max(width-1-p.inspW, 0), width
	} else {
		p.inspW = width - 1 - p.split
	}
	proc := ps[sel]
	ui.Split(c, &p.split, func() { p.table(c, in).Fill() }, func() {
		ui.Scroll(c).Fill().Children(func() { p.inspector(c, in, &proc) })
	}).Height(h)
}

// visible reports whether process i passes the search.
func (p *Processes) visible(i int) bool {
	if p.treeMode {
		return true // the tree's own rows decide; see table
	}
	return slices.Contains(p.cache.rows, i)
}

// table draws the process table or tree, and takes what the user did to it:
// a choice, a sort, a moved or hidden column.
func (p *Processes) table(c *ui.Context, in ProcessesInput) ui.Element {
	ps := in.View.Procs
	cols := make([]ui.TableColumn, len(p.cols))
	for i, id := range p.cols {
		d, _ := colByID(id)
		cols[i] = ui.TableColumn{Title: d.title, ID: id, Width: d.width, MinWidth: d.min, Align: d.align, Sortable: true}
	}
	cell := func(pr *model.Process, col int) { procCell(c, pr, p.cols[col], in.Now) }

	prevSort := p.sort
	p.row = -1
	var el ui.Element
	var layout *ui.TableLayout
	var chosen func(row int) (procKey, bool)
	if p.treeMode {
		st := &p.tree
		for k := range p.cache.kids {
			if !p.collapsed[k] {
				st.Open.Add(k)
			}
		}
		if p.hasSel {
			for r := 0; r < st.Rows(); r++ {
				if st.Item(r) == p.sel {
					p.row = r
					break
				}
			}
		}
		st.List.Selected, st.List.Sort, st.List.Columns = &p.row, &p.sort, p.layout
		layout = &st.List.Columns
		el = ui.OutlineTable(c, st, cols, p.cache.roots, func(k procKey) []procKey { return p.cache.kids[k] },
			func(k procKey, col int) {
				i, ok := p.cache.index[k]
				if !ok || i >= len(ps) {
					return
				}
				if n := len(p.cache.kids[k]); n > 0 && p.cols[col] == "name" {
					nameWithCount(c, ps[i].Name, n)
					return
				}
				cell(&ps[i], col)
			})
		chosen = func(row int) (procKey, bool) { return st.Item(row), row < st.Rows() }
	} else {
		st := &p.list
		rows := p.cache.rows
		if p.hasSel {
			p.row = slices.IndexFunc(rows, func(i int) bool { return p.cache.keys[i] == p.sel })
		}
		st.Selected, st.Sort, st.Columns = &p.row, &p.sort, p.layout
		st.Key = func(r int) any { return p.cache.keys[rows[r]] }
		st.Label = func(r int) string { return ps[rows[r]].Name }
		layout = &st.Columns
		el = ui.Table(c, st, cols, len(rows), func(r, col int) { cell(&ps[rows[r]], col) })
		chosen = func(row int) (procKey, bool) { return p.cache.keys[rows[row]], row < len(rows) }
	}
	el.Changed() // applies this frame's click, key or header drag now
	if p.treeMode {
		if p.collapsed == nil {
			p.collapsed = map[procKey]bool{}
		}
		for k := range p.cache.kids {
			if p.tree.Open.Has(k) {
				delete(p.collapsed, k)
			} else {
				p.collapsed[k] = true
			}
		}
		if p.hasSel && p.row < 0 && p.tree.Rows() > 0 { // collapsed away or filtered out
			p.hasSel = false
			c.Invalidate()
		}
	} else if p.hasSel && p.row < 0 { // filtered out
		p.hasSel = false
		c.Invalidate()
	}
	if p.row >= 0 {
		if k, ok := chosen(p.row); ok {
			p.sel, p.hasSel = k, true
		}
	}
	p.layout = *layout

	// A click on a new column's header sorts it ascending; sorting starts
	// with the largest.
	if p.sort.Column != prevSort.Column && !p.sort.Descending {
		p.sort.Descending = true
	}
	if p.sort != prevSort {
		c.Invalidate()
	}
	p.columnsChanged()
	el.ContextMenu(func(m *ui.Menu) {
		for _, d := range procCols {
			visible := slices.Contains(p.cols, d.id)
			if m.Item(d.title).Checked(visible).Disabled(visible && len(p.cols) == 1).Chosen() {
				p.toggled = d.id
			}
		}
	})
	if id := p.toggled; id != "" {
		p.toggled = ""
		if slices.Contains(p.cols, id) {
			p.setColumns(slices.DeleteFunc(slices.Clone(p.cols), func(s string) bool { return s == id }))
		} else {
			p.setColumns(append(slices.Clone(p.cols), id))
		}
		c.Invalidate()
	}
	return el
}

// columnsChanged adopts the order the user dragged the columns to.
func (p *Processes) columnsChanged() {
	if len(p.layout.Order) == 0 {
		return
	}
	var order []string
	for _, id := range p.layout.Order {
		if slices.Contains(p.cols, id) && !slices.Contains(order, id) {
			order = append(order, id)
		}
	}
	for _, id := range p.cols {
		if !slices.Contains(order, id) {
			order = append(order, id)
		}
	}
	if !slices.Equal(order, p.cols) {
		p.setColumns(order)
	}
}

func portsText(ports []model.Port) string {
	s := make([]string, len(ports))
	for i, q := range ports {
		s[i] = fmt.Sprintf("%d/%s", q.Number, q.Proto)
	}
	return strings.Join(s, ", ")
}

func orDash(s string) string {
	if s == "" {
		return format.Unknown
	}
	return s
}

func procCell(c *ui.Context, pr *model.Process, id string, now time.Time) {
	var s string
	switch id {
	case "name":
		s = orDash(pr.Name)
	case "pid":
		s = pid(*pr)
	case "user":
		s = orDash(pr.User)
	case "cpu":
		s = format.Percent(&pr.CPUPct)
	case "mem":
		s = format.Unknown
		if pr.RSS > 0 {
			s = format.Bytes(pr.RSS)
		}
	case "threads":
		s = format.Unknown
		if pr.Threads > 0 {
			s = fmt.Sprint(pr.Threads)
		}
	case "ports":
		s = portsText(pr.Ports)
	case "started":
		s = format.Unknown
		if !pr.StartTime.IsZero() {
			l, n := pr.StartTime.Local(), now.Local()
			if l.YearDay() == n.YearDay() && l.Year() == n.Year() {
				s = l.Format("15:04:05")
			} else {
				s = l.Format("Jan 2 15:04")
			}
		}
	}
	// Heavy consumers stand out: half a core or more, or 2 GB and up.
	col := c.Theme().Text
	if (id == "cpu" && pr.CPUPct >= 50) || (id == "mem" && pr.RSS >= 2<<30) {
		col = c.Theme().Warning
	}
	ui.Text(c, s).FontSize(13).TextColor(col).SingleLine()
}

// scopeLabels are the process scopes the toolbar offers, in order.
var scopeLabels = []string{"All", "Apps", "Mine", "System"}

// currentUser is the account running hotwatch, for the Mine scope; on a
// remote host it matches only an account of the same name.
var currentUser = func() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}()

// SetCurrentUser overrides the account the Mine scope matches (tests).
func SetCurrentUser(name string) { currentUser = name }

// inScope reports whether p belongs to scope: every process; application
// bundles; the current user's; or system accounts (root, and the service
// accounts macOS names with a leading underscore).
func inScope(p *model.Process, scope int) bool {
	switch scope {
	case 1:
		return p.AppBundle != ""
	case 2:
		return currentUser != "" && p.User == currentUser
	case 3:
		return p.User == "root" || strings.HasPrefix(p.User, "_")
	}
	return true
}

// footer is the line under the table: how many processes are listed, and
// the host's CPU and memory in use.
func (p *Processes) footer(c *ui.Context, in ProcessesInput) {
	t := c.Theme()
	ps := in.View.Procs
	count := fmt.Sprintf("%d processes", len(ps))
	if p.cache.shown != len(ps) {
		count = fmt.Sprintf("%d of %d processes", p.cache.shown, len(ps))
	}
	ui.Row(c).AlignItems(ui.Center).Gap(2 * g()).Children(func() {
		ui.Text(c, count).FontSize(12).TextColor(t.TextMuted).NoWrap().Grow(1)
		if s := in.View.Snap; s != nil {
			ui.Text(c, fmt.Sprintf("CPU %.0f%%", s.CPU.Total)).FontSize(12).TextColor(t.TextMuted).NoWrap()
			if s.Memory.Total > 0 {
				ui.Text(c, "Memory "+format.Bytes(s.Memory.Used)).FontSize(12).TextColor(t.TextMuted).NoWrap()
			}
		}
	})
}

// nameWithCount is a tree group's name with the number of processes under it.
func nameWithCount(c *ui.Context, name string, n int) {
	t := c.Theme()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, orDash(name)).FontSize(13).SingleLine()
		ui.Row(c).Padding(1, 6).Radius(8).Background(t.SurfaceHover).Children(func() {
			ui.Text(c, fmt.Sprint(n)).FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
	})
}
