package pages

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/mac/apps"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

var appCols = []ui.TableColumn{
	{Title: "Name", ID: "name", MinWidth: 160, Sortable: true},
	{Title: "Version", ID: "version", Width: 220, MinWidth: 90, Sortable: true},
	{Title: "Size", ID: "size", Width: 90, MinWidth: 60, Align: ui.End, Sortable: true},
	{Title: "Kind", ID: "kind", Width: 140, MinWidth: 80, Sortable: true},
	{Title: "Last opened", ID: "opened", Width: 140, MinWidth: 90, Sortable: true},
	{Title: "Source", ID: "source", Width: 120, MinWidth: 70, Sortable: true},
}

// AppsInput is what the Apps page reads each frame.
type AppsInput struct {
	HostID  string
	Now     time.Time
	Actions *Actions
	Width   float32
	// Procs are the host's processes: an app with one is running.
	Procs []model.Process
	// Packages is the Packages view: the Homebrew and mise tabs, and which
	// cask installed an app and whether it has an update. nil: none.
	Packages *Packages
}

// appTabs are the Apps page's views.
var appTabs = []string{"Apps", "Homebrew", "mise"}

// Apps is the Apps page: the applications installed on this Mac, with an
// inspector and an uninstall that moves the bundle to the Trash. The scan
// and the sizes run off the main thread and land through post.
type Apps struct {
	// Open opens the bundle (the wiring runs `open`); nil hides the button.
	Open func(bundle string)
	// Reveal shows the bundle in the Finder; nil hides the button.
	Reveal func(bundle string)
	// Spawn runs f off the main thread; tests make it run inline.
	Spawn func(f func())

	scanner apps.Scanner
	post    func(func())

	search ui.Handle
	query  string
	list   ui.ListState
	sort   ui.SortOrder
	layout ui.TableLayout
	row    int
	sel    string // selected bundle

	loading, loaded bool
	apps            []apps.App
	err             error
	rows            []int // indexes into apps after search and sort
	rowsKey         string
	sizes           map[string]uint64
	sizing          map[string]bool
	ask             bool
	pending         apps.App
	handled         hub.ActionID    // the last uninstall whose result reloaded the list
	running         map[string]bool // bundles with a running process, this frame
	tab             int             // index into appTabs
	pk              *Packages       // this frame's Packages view
}

// NewApps returns the page over scanner; post runs a function on the main
// thread. A nil scanner (a remote host) shows a plain explanation.
func NewApps(scanner apps.Scanner, post func(func())) *Apps {
	p := &Apps{scanner: scanner, post: post, row: -1, sizes: map[string]uint64{}, sizing: map[string]bool{},
		sort: ui.SortOrder{Column: "name"}}
	p.Spawn = func(f func()) { go f() }
	if post == nil {
		p.post = func(f func()) { f() }
	}
	return p
}

// Reload scans again on the next frame.
func (p *Apps) Reload() { p.loaded, p.loading = false, false }

func (p *Apps) load() {
	p.loading = true
	scanner := p.scanner
	p.Spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		list, err := scanner.Apps(ctx)
		p.post(func() {
			p.apps, p.err, p.loaded, p.loading = list, err, true, false
			p.rowsKey = ""
		})
	})
}

// size asks for a bundle's size once; the answer lands through post.
func (p *Apps) size(bundle string) {
	if _, ok := p.sizes[bundle]; ok || p.sizing[bundle] {
		return
	}
	p.sizing[bundle] = true
	scanner := p.scanner
	p.Spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		n, err := scanner.Size(ctx, bundle)
		p.post(func() {
			delete(p.sizing, bundle)
			if err == nil {
				p.sizes[bundle] = n
			}
		})
	})
}

// ensure recomputes the visible rows when the search or sort changed.
func (p *Apps) ensure() {
	key := fmt.Sprintf("%s|%s|%v|%d|%d", p.query, p.sort.Column, p.sort.Descending, len(p.apps), len(p.sizes))
	if key == p.rowsKey {
		return
	}
	p.rowsKey = key
	p.rows = p.rows[:0]
	q := strings.ToLower(strings.TrimSpace(p.query))
	for i, a := range p.apps {
		if q == "" || strings.Contains(strings.ToLower(a.Name), q) || strings.Contains(strings.ToLower(a.BundleID), q) {
			p.rows = append(p.rows, i)
		}
	}
	less := func(a, b apps.App) int {
		switch p.sort.Column {
		case "version":
			return strings.Compare(a.Version, b.Version)
		case "size":
			return cmpU(p.sizes[a.Bundle], p.sizes[b.Bundle])
		case "kind":
			return strings.Compare(a.Arch, b.Arch)
		case "opened":
			return a.LastOpened.Compare(b.LastOpened)
		case "source":
			return strings.Compare(a.Source, b.Source)
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	}
	slices.SortStableFunc(p.rows, func(i, j int) int {
		c := less(p.apps[i], p.apps[j])
		if p.sort.Descending {
			c = -c
		}
		return c
	})
}

func cmpU(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Render builds the page.
func (p *Apps) Render(c *ui.Context, in AppsInput) {
	if p.scanner == nil {
		components.EmptyState(c, "Apps are listed for this Mac only")
		return
	}
	if !p.loaded && !p.loading {
		p.load()
	}
	p.reloadAfterUninstall(in)
	p.pk = in.Packages
	p.pk.ensureLoaded()
	p.ensure()
	p.running = map[string]bool{}
	for _, pr := range in.Procs {
		if pr.AppBundle != "" {
			p.running[outerBundle(pr.AppBundle)] = true
		}
	}
	t := c.Theme()
	ui.Column(c).Gap(2 * g()).Children(func() {
		ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Children(func() {
			if p.loading {
				ui.Text(c, "Scanning…").FontSize(13).TextColor(t.TextMuted)
			} else {
				ui.Text(c, p.summary()).FontSize(13).TextColor(t.TextMuted).SingleLine()
			}
			ui.Spacer(c)
			if p.tab == 0 {
				ui.SearchField(c, &p.query).Label("Search apps").Bind(&p.search).Width(36 * g())
			}
			if in.Packages != nil && in.Packages.mgr != nil {
				ui.Segmented(c, &p.tab, appTabs...)
			}
		})
		if p.tab > 0 && in.Packages != nil {
			in.Packages.embedded, in.Packages.tab = true, p.tab-1
			in.Packages.Render(c, PackagesInput{HostID: in.HostID, Now: in.Now, Actions: in.Actions, Width: in.Width})
			return
		}
		if p.err != nil {
			components.Banner(c, components.BannerWarning, "Could not list applications: "+p.err.Error())
		}
		if p.loaded && len(p.apps) == 0 {
			components.EmptyState(c, "No applications found")
			return
		}
		_, wh := c.Size()
		h := max(wh-260, 240)
		sel, hasSel := p.selected()
		if hasSel && in.Width > 700 {
			ui.Row(c).Gap(2 * g()).AlignItems(ui.Stretch).Height(h).Children(func() {
				p.table(c).Grow(1)
				components.Surface(c, ui.Column(c)).Width(44 * g()).Children(func() { p.inspector(c, in, sel) })
			})
		} else {
			p.table(c).Height(h)
		}
		p.footer(c, in)
	})
	p.dialog(c, in)
}

// selected is the chosen application, if it is still listed.
func (p *Apps) selected() (apps.App, bool) {
	if p.sel == "" {
		return apps.App{}, false
	}
	for _, a := range p.apps {
		if a.Bundle == p.sel {
			return a, true
		}
	}
	return apps.App{}, false
}

func (p *Apps) table(c *ui.Context) ui.Element {
	st := &p.list
	// The table writes the clicked row into p.row before the next frame
	// builds; p.row is only recomputed when a selection is already known.
	if p.sel != "" {
		p.row = -1
		for r, i := range p.rows {
			if p.apps[i].Bundle == p.sel {
				p.row = r
				break
			}
		}
	}
	st.Selected, st.Sort, st.Columns = &p.row, &p.sort, p.layout
	st.Key = func(r int) any { return p.apps[p.rows[r]].Bundle }
	st.Label = func(r int) string { return p.apps[p.rows[r]].Name }
	prev := p.sort
	el := ui.Table(c, st, appCols, len(p.rows), func(r, col int) { p.cell(c, &p.apps[p.rows[r]], appCols[col].ID) })
	el.Changed() // applies this frame's click or header sort now
	if p.sort != prev {
		p.rowsKey = ""
	}
	if p.row >= 0 && p.row < len(p.rows) {
		p.sel = p.apps[p.rows[p.row]].Bundle
	} else if p.row < 0 {
		p.sel = ""
	}
	return el
}

func (p *Apps) cell(c *ui.Context, a *apps.App, id string) {
	t := c.Theme()
	text := func(s string, end bool) {
		e := ui.Text(c, s).FontSize(13).SingleLine()
		if end {
			e.TextAlign(ui.End)
		}
	}
	switch id {
	case "name":
		ui.Row(c).Gap(components.Grid).AlignItems(ui.Center).Children(func() {
			appIcon(c, a.Name, a.Bundle)
			ui.Text(c, a.Name).FontSize(13).SingleLine()
			if p.running[a.Bundle] {
				ui.Box(c).Size(7, 7).Radius(3.5).Background(t.Success).Label("running")
			}
		})
	case "version":
		if x, ok := p.pk.caskFor(a.Bundle); ok && x.Outdated && x.Newest != "" {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, orDash(a.Version)).FontSize(13).SingleLine()
				ui.Text(c, "→").FontSize(12).TextColor(t.TextMuted).SingleLine()
				ui.Text(c, x.Newest).FontSize(13).FontWeight(650).TextColor(ui.Hex("#0a84ff")).SingleLine()
			})
			return
		}
		text(orDash(a.Version), false)
	case "size":
		if n, ok := p.sizes[a.Bundle]; ok {
			text(format.Bytes(n), true)
		} else {
			p.size(a.Bundle) // a visible row asks for its size
			ui.Text(c, "…").FontSize(13).TextColor(t.TextMuted).SingleLine().TextAlign(ui.End)
		}
	case "kind":
		text(orDash(a.Arch), false)
	case "opened":
		text(openedText(a.LastOpened), false)
	case "source":
		src := a.Source
		if _, ok := p.pk.caskFor(a.Bundle); ok && src == "" {
			src = "Homebrew"
		}
		text(orDash(src), false)
	}
}

// openedText is "Today", "Yesterday", "Oct 2" or "—".
func openedText(t time.Time) string {
	if t.IsZero() {
		return format.Unknown
	}
	now := time.Now()
	y, m, d := t.Local().Date()
	ny, nm, nd := now.Date()
	switch {
	case y == ny && m == nm && d == nd:
		return "Today"
	case t.After(now.Add(-48 * time.Hour)):
		return "Yesterday"
	case y == ny:
		return t.Local().Format("Jan 2")
	}
	return t.Local().Format("Jan 2, 2006")
}

// summary is "31 apps · 21.2 GB · 4 running": the size once every bundle
// is measured.
func (p *Apps) summary() string {
	var total uint64
	sized := 0
	for _, a := range p.apps {
		if n, ok := p.sizes[a.Bundle]; ok {
			total += n
			sized++
		}
	}
	s := plural(len(p.apps), "app")
	if sized == len(p.apps) && sized > 0 {
		s += " · " + format.Bytes(total)
	}
	n := 0
	for _, a := range p.apps {
		if p.running[a.Bundle] {
			n++
		}
	}
	if n > 0 {
		s += fmt.Sprintf(" · %d running", n)
	}
	if u := p.updates(); u > 0 {
		s += " · " + plural(u, "update")
	}
	return s
}

// updates counts the listed apps whose cask has a newer version.
func (p *Apps) updates() int {
	n := 0
	for _, a := range p.apps {
		if x, ok := p.pk.caskFor(a.Bundle); ok && x.Outdated {
			n++
		}
	}
	return n
}

// footer says when Homebrew was last asked and offers to update every app
// it installed.
func (p *Apps) footer(c *ui.Context, in AppsInput) {
	if p.pk == nil || p.pk.mgr == nil {
		return
	}
	t := c.Theme()
	ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		switch {
		case p.pk.loading:
			ui.Text(c, "Checking for updates…").FontSize(13).TextColor(t.TextMuted).SingleLine()
		case !p.pk.checked.IsZero():
			ui.Text(c, "Checked "+format.Ago(in.Now.Sub(p.pk.checked))).FontSize(13).TextColor(t.TextMuted).SingleLine()
		}
		u := p.updates()
		if u == 0 {
			return
		}
		ui.Text(c, plural(u, "update")).FontSize(13).TextColor(t.TextMuted).SingleLine()
		last, started := in.Actions.Last(in.HostID, "package")
		busy := started && (last.State == "running" || last.State == "confirming")
		all := ui.Row(c).Key("apps-update-all").Cursor(ui.CursorPointer).Label("Update All")
		if all.Clicked() && !busy {
			in.Actions.Start(hub.Action{Kind: "package", HostID: in.HostID, State: "confirming", Verb: "upgrade-all", Target: "cask:"})
		}
		col := ui.Hex("#0a84ff")
		if busy {
			col = t.TextMuted
		}
		all.Children(func() { ui.Text(c, "Update All").FontSize(13).FontWeight(650).TextColor(col).SingleLine() })
	})
}

// inspector shows the chosen application: its icon and name, the actions
// (open, move to the Trash, show in the Finder), its details and its path.
func (p *Apps) inspector(c *ui.Context, in AppsInput, a apps.App) {
	t := c.Theme()
	running := in.Actions.Running(in.HostID, a.Bundle)
	ui.Column(c).Gap(2 * g()).Children(func() {
		ui.Row(c).Gap(g() + 4).AlignItems(ui.Center).Children(func() {
			appIconSized(c, a.Name, a.Bundle, 48)
			ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, a.Name).FontSize(18).FontWeight(700).SingleLine()
				if a.BundleID != "" {
					ui.Text(c, a.BundleID).FontSize(13).TextColor(t.TextMuted).SingleLine()
				}
			})
			closeBtn := ui.Row(c).Key("app-close").Size(24, 24).Center().Radius(12).Cursor(ui.CursorPointer).Label("Close")
			if closeBtn.Hovered() {
				closeBtn.Background(t.SurfaceHover)
			}
			if closeBtn.Clicked() {
				p.sel, p.row = "", -1
			}
			closeBtn.Children(func() { components.Glyph(c, icons.Close, 14, t.TextMuted) })
		})
		cask, isCask := p.pk.caskFor(a.Bundle)
		if isCask && cask.Desc != "" {
			ui.Text(c, cask.Desc).FontSize(14)
		}
		if isCask && cask.Outdated && cask.Newest != "" {
			last, started := in.Actions.Last(in.HostID, "package")
			busy := started && (last.State == "running" || last.State == "confirming")
			if ui.PrimaryButton(c, "Update to "+cask.Newest).Disabled(busy).Clicked() {
				in.Actions.Start(hub.Action{Kind: "package", HostID: in.HostID, State: "confirming", Verb: "upgrade", Target: "cask:" + cask.Name})
			}
		}
		ui.Row(c).Gap(g()).Children(func() {
			if p.Open != nil && ui.Button(c, "Open").Grow(1).Clicked() {
				p.Open(a.Bundle)
			}
			system := strings.HasPrefix(a.Bundle, "/System/")
			trash := ui.Button(c, "Move to Trash…").Grow(1).Disabled(running || system)
			if !running && !system {
				trash.TextColor(t.Danger)
			}
			if trash.Clicked() {
				p.pending, p.ask = a, true
			}
			if p.Reveal != nil {
				rv := ui.Row(c).Key("app-reveal").Padding(6, 10).Radius(8).Border(1, t.Border).Center().Cursor(ui.CursorPointer).Label("Show in Finder")
				if rv.Clicked() {
					p.Reveal(a.Bundle)
				}
				rv.Children(func() { components.Glyph(c, icons.Folder, 15, t.TextMuted) })
			}
		})
		ui.Text(c, "DETAILS").FontSize(12).FontWeight(650).TextColor(t.TextMuted).Margin(g(), 0, 0, 0)
		ui.Column(c).Gap(g()).Children(func() {
			kvRow(c, "Version", orDash(a.Version))
			if isCask && cask.Newest != "" {
				kvRow(c, "Newest", cask.Newest)
			}
			if n, ok := p.sizes[a.Bundle]; ok {
				kvRow(c, "Size", format.Bytes(n))
			} else {
				kvRow(c, "Size", "…")
			}
			kvRow(c, "Kind", orDash(a.Arch))
			opened := "Never"
			if !a.LastOpened.IsZero() {
				opened = a.LastOpened.Local().Format("Jan 2, 2006 15:04")
			}
			kvRow(c, "Last opened", opened)
			kvRow(c, "Source", orDash(a.Source))
			switch {
			case isCask && cask.AutoUpdates:
				kvRow(c, "Updates", "The app itself")
			case isCask:
				kvRow(c, "Updates", "Homebrew")
			case a.Source == "App Store":
				kvRow(c, "Updates", "App Store")
			default:
				kvRow(c, "Updates", "Unknown")
			}
			if isCask {
				kvRow(c, "Cask", cask.Name)
				if !cask.Installed.IsZero() {
					kvRow(c, "Installed", cask.Installed.Local().Format("Jan 2, 2006"))
				}
				if cask.Tap != "" {
					kvRow(c, "Tap", cask.Tap)
				}
				if home := cask.Homepage; home != "" && p.Open != nil && (strings.HasPrefix(home, "https://") || strings.HasPrefix(home, "http://")) {
					ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Homepage").FontSize(14).TextColor(t.TextMuted).SingleLine().Shrink(0)
						ui.Spacer(c)
						link := ui.Text(c, strings.TrimPrefix(strings.TrimPrefix(home, "https://"), "http://")).FontSize(14).FontWeight(600).
							TextColor(ui.Hex("#0a84ff")).SingleLine().Shrink(1).Cursor(ui.CursorPointer).Key("app-home")
						if link.Clicked() {
							p.Open(home)
						}
					})
				}
			}
			state := "Not running"
			if p.running[a.Bundle] {
				state = "Running"
			}
			kvRow(c, "State", state)
		})
		ui.Text(c, "PATH").FontSize(12).FontWeight(650).TextColor(t.TextMuted).Margin(g(), 0, 0, 0)
		ui.Text(c, a.Bundle).FontSize(13).Font("Menlo").TextColor(t.TextMuted).Selectable()
		if act, ok := in.Actions.LastOn(in.HostID, "uninstall", a.Bundle); ok {
			col := t.TextMuted
			if act.State == "failed" {
				col = t.Danger
			}
			ui.Text(c, actionResult(act)).FontSize(12).TextColor(col)
		}
	})
}

// dialog confirms the uninstall; only its confirm button starts the action.
func (p *Apps) dialog(c *ui.Context, in AppsInput) {
	a := p.pending
	if ui.AlertDialog(c, &p.ask, "Move “"+a.Name+"” to the Trash?",
		"The application is moved to the Trash, where it can be put back. macOS asks for an administrator name if the bundle is not yours.",
		"Cancel", "Move to Trash") == 1 {
		in.Actions.Start(hub.Action{Kind: "uninstall", HostID: in.HostID, State: "confirming", Verb: "uninstall", Target: a.Bundle})
	}
}

// reloadAfterUninstall rescans once an uninstall finished.
func (p *Apps) reloadAfterUninstall(in AppsInput) {
	act, ok := in.Actions.Last(in.HostID, "uninstall")
	if !ok || act.ID == p.handled || act.State == "running" || act.State == "confirming" {
		return
	}
	p.handled = act.ID
	if act.State == "succeeded" {
		p.sel = ""
		p.Reload()
	}
}
