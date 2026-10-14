// Package ui is hotwatch's native window content.
package ui

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/store"
	"hotwatch/ui/components"
	"hotwatch/ui/icons"
	"hotwatch/ui/pages"
)

// App holds everything the view reads. It is touched only on the main thread:
// Apply is called inside win.Update.
type App struct {
	hosts    []store.Host
	actions  hub.Runner
	onSelect func(hostID, page string)

	panel HostPanel // nil: the placeholder list

	selected string
	page     string
	views    map[string]hub.HostView
	gen      uint64 // bumped by Apply: pages recompute only when it changes
	now      func() time.Time

	acts      *pages.Actions
	procs     *pages.Processes
	docker    *pages.Docker
	onColumns func(page string, cols []string)

	// Shell state: the top bar's tab index into tabKeys (the last frame's
	// visible pages), and the split between content and host panel.
	tab     int
	tabKeys []string
	split   float32 // content width
	panelW  float32
	lastW   float32 // window width the split was last fitted to
}

// NewApp seeds the per-host state from views(id) for each host. The first host
// is selected and the Overview page shown.
func NewApp(views func(id string) hub.HostView, hosts []store.Host, actions hub.Runner, onSelect func(hostID, page string)) *App {
	a := &App{hosts: slices.Clone(hosts), actions: actions, onSelect: onSelect, page: "overview",
		views: map[string]hub.HostView{}, now: time.Now, panelW: DefaultPanelWidth,
		acts: pages.NewActions(actions), procs: pages.NewProcesses(), docker: pages.NewDocker()}
	a.procs.OnColumns = func(cols []string) {
		if a.onColumns != nil {
			a.onColumns("processes", cols)
		}
	}
	for _, h := range a.hosts {
		a.views[h.ID] = copyView(views(h.ID))
	}
	if len(a.hosts) > 0 {
		a.selected = a.hosts[0].ID
	}
	return a
}

// Apply stores a copy of v for host id. Call it on the main thread.
func (a *App) Apply(id string, v hub.HostView) {
	a.views[id] = copyView(v)
	a.gen++
}

// ApplyAction records an action snapshot published by the runner, keeping
// the highest Seq per action. Call it on the main thread.
func (a *App) ApplyAction(act hub.Action) { a.acts.Apply(act) }

// SetLogsOpener sets how the Docker page reaches container logs.
func (a *App) SetLogsOpener(o pages.LogsOpener) { a.docker.SetLogsOpener(o) }

// SetPost sets how the log reader hands work to the main thread, such as
// win.Update. Without one the work runs on the reader's goroutine.
func (a *App) SetPost(post func(func())) { a.docker.SetPost(post) }

// SetColumnsHook sets the function called with a page's visible column ids,
// in order, when the user hides, shows or moves one; the caller persists them.
func (a *App) SetColumnsHook(f func(page string, cols []string)) { a.onColumns = f }

// SetSavedColumns restores a page's saved visible columns.
func (a *App) SetSavedColumns(page string, cols []string) {
	if page == "processes" {
		a.procs.SetColumns(cols)
	}
}

// copyView detaches the snapshot and slices from the poller's data.
// Convention: published views are never mutated in place, so pointer fields
// (rates, GPU values) and inner slices (Addrs, Assertions) are shared
// read-only; only the containers Apply stores are cloned.
// Hist is internally synchronised and shared.
func copyView(v hub.HostView) hub.HostView {
	if v.Snap != nil {
		s := *v.Snap
		s.CPU.PerCore = slices.Clone(s.CPU.PerCore)
		s.Disks = slices.Clone(s.Disks)
		s.Net = slices.Clone(s.Net)
		s.GPUs = slices.Clone(s.GPUs)
		s.Partial = slices.Clone(s.Partial)
		if s.Battery != nil {
			b := *s.Battery
			s.Battery = &b
		}
		v.Snap = &s
	}
	v.Procs = slices.Clone(v.Procs)
	v.Ctrs = slices.Clone(v.Ctrs)
	return v
}

// HostPanel draws the right-hand host panel: selected is the host the pages
// show and view returns any host's latest view.
type HostPanel interface {
	Render(c *ui.Context, selected string, view func(id string) hub.HostView)
}

// SetHostPanel replaces the placeholder host list with p.
func (a *App) SetHostPanel(p HostPanel) { a.panel = p }

func (a *App) host() (store.Host, bool) {
	for _, h := range a.hosts {
		if h.ID == a.selected {
			return h, true
		}
	}
	return store.Host{}, false
}

// setPage opens page on the selected host.
func (a *App) setPage(page string) {
	if page == a.page {
		return
	}
	a.docker.CloseLogs()
	a.page = page
	a.onSelect(a.selected, page)
}

// selectHost switches every page to host id.
func (a *App) selectHost(id string) {
	if id == a.selected {
		return
	}
	a.docker.CloseLogs()
	a.selected = id
	a.onSelect(id, a.page)
}

// View builds the window content.
func (a *App) View(c *ui.Context) {
	v := a.views[a.selected]
	Shell(c, a, a.drawHostPanel, func() {
		switch a.page {
		case "overview":
			pages.Overview(c, v, a.now())
		case "processes":
			a.procs.Render(c, pages.ProcessesInput{HostID: a.selected, View: v, Gen: a.gen, Now: a.now(),
				Width: a.split - 6*components.Grid, Actions: a.acts})
		case "docker":
			a.docker.Render(c, pages.DockerInput{HostID: a.selected, View: v, Gen: a.gen, Now: a.now(), Actions: a.acts})
		default:
			components.EmptyState(c, "This page is not built yet.")
		}
	})
}

func (a *App) drawHostPanel(c *ui.Context) {
	if a.panel != nil {
		a.panel.Render(c, a.selected, func(id string) hub.HostView { return a.views[id] })
		return
	}
	a.placeholderPanel(c)
}

// placeholderPanel is a flat list of the hosts, shown until SetHostPanel.
func (a *App) placeholderPanel(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Padding(2*components.Grid, components.Grid).Gap(2).Children(func() {
		ui.Text(c, "Hosts").FontSize(12).FontWeight(600).TextColor(t.TextMuted).Padding(0, components.Grid, components.Grid, components.Grid)
		for _, h := range a.hosts {
			row := ui.Row(c).Key(h.ID).Gap(components.Grid).Padding(6, components.Grid).Radius(components.Grid)
			if h.ID == a.selected {
				row.Background(t.Accent.Alpha(0.12))
			} else if row.Hovered() {
				row.Background(t.SurfaceHover)
			}
			if row.Clicked() {
				a.selectHost(h.ID)
			}
			row.Children(func() {
				ui.Icon(c, icons.Host).FontSize(14).TextColor(t.TextMuted)
				ui.Text(c, h.Name).FontSize(13).SingleLine().Grow(1)
				if st := a.views[h.ID].Conn.State; st != "" {
					ui.Box(c).Size(8, 8).Radius(4).Background(components.StateColor(t, string(st)))
				}
			})
		}
	})
}
