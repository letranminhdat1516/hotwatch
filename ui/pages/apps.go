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
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

var appCols = []ui.TableColumn{
	{Title: "Name", ID: "name", MinWidth: 160, Sortable: true},
	{Title: "Version", ID: "version", Width: 110, MinWidth: 70, Sortable: true},
	{Title: "Size", ID: "size", Width: 90, MinWidth: 60, Align: ui.End, Sortable: true},
	{Title: "Kind", ID: "kind", Width: 110, MinWidth: 80, Sortable: true},
	{Title: "Last opened", ID: "opened", Width: 120, MinWidth: 90, Sortable: true},
	{Title: "Source", ID: "source", Width: 100, MinWidth: 70, Sortable: true},
}

// AppsInput is what the Apps page reads each frame.
type AppsInput struct {
	HostID  string
	Now     time.Time
	Actions *Actions
	Width   float32
}

// Apps is the Apps page: the applications installed on this Mac, with an
// inspector and an uninstall that moves the bundle to the Trash. The scan
// and the sizes run off the main thread and land through post.
type Apps struct {
	// Open opens the bundle (the wiring runs `open`); nil hides the button.
	Open func(bundle string)
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
	handled         hub.ActionID // the last uninstall whose result reloaded the list
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
	p.ensure()
	t := c.Theme()
	ui.Column(c).Gap(2 * g()).Children(func() {
		ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Children(func() {
			ui.SearchField(c, &p.query).Label("Search apps").Bind(&p.search).Width(32 * g())
			ui.Spacer(c)
			if p.loading {
				ui.Text(c, "Scanning…").FontSize(12).TextColor(t.TextMuted)
			}
		})
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
				components.Surface(c, ui.Column(c)).Width(40 * g()).Children(func() { p.inspector(c, in, sel) })
			})
		} else {
			p.table(c).Height(h)
		}
		p.footer(c)
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
		})
	case "version":
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
		text(orDash(a.Source), false)
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

func (p *Apps) footer(c *ui.Context) {
	t := c.Theme()
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
	ui.Text(c, s).FontSize(12).TextColor(t.TextMuted)
}

// inspector shows the chosen application and its actions.
func (p *Apps) inspector(c *ui.Context, in AppsInput, a apps.App) {
	t := c.Theme()
	running := in.Actions.Running(in.HostID, a.Bundle)
	ui.Column(c).Gap(2 * g()).Children(func() {
		ui.Row(c).Gap(g()).AlignItems(ui.Center).Children(func() {
			appIcon(c, a.Name, a.Bundle)
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, a.Name).FontSize(15).FontWeight(600).SingleLine()
				if a.BundleID != "" {
					ui.Text(c, a.BundleID).FontSize(12).TextColor(t.TextMuted).SingleLine()
				}
			})
		})
		ui.Row(c).Gap(g()).Children(func() {
			if p.Open != nil && ui.Button(c, "Open").Clicked() {
				p.Open(a.Bundle)
			}
			system := strings.HasPrefix(a.Bundle, "/System/")
			if ui.Button(c, "Uninstall").Disabled(running || system).Clicked() {
				p.pending, p.ask = a, true
			}
		})
		field := func(label, value string) {
			if value == "" {
				return
			}
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
				ui.Text(c, value).FontSize(13).Selectable()
			})
		}
		field("Version", a.Version)
		if n, ok := p.sizes[a.Bundle]; ok {
			field("Size", format.Bytes(n))
		}
		field("Kind", a.Arch)
		if !a.LastOpened.IsZero() {
			field("Last opened", a.LastOpened.Local().Format("Jan 2, 2006 15:04"))
		}
		field("Source", a.Source)
		field("Path", a.Bundle)
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
