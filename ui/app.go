// Package ui is hotwatch's native window content.
package ui

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/mac/appicon"
	"hotwatch/mac/apps"
	"hotwatch/mac/pkgs"
	"hotwatch/store"
	"hotwatch/ui/components"
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
	nextHost string // host a shortcut chose, selected at the start of the next frame
	views    map[string]hub.HostView
	gen      uint64 // bumped by Apply: pages recompute only when it changes
	now      func() time.Time

	acts      *pages.Actions
	procs     *pages.Processes
	docker    *pages.Docker
	apps      *pages.Apps
	pkgs      *pages.Packages
	post      func(func()) // SetPost: the main-thread hop handed to pages
	onColumns func(page string, cols []string)

	settings   store.Settings       // shown on the Settings page; NetUnits is applied each frame
	onSettings func(store.Settings) // SetSettingsHook
	settingsUI pages.SettingsState  // the page's bound controls, kept between frames

	notice   string                // SetNotice; "" when dismissed
	onFrame  func(start time.Time) // SetFrameHook
	prompts  []*passPrompt         // AskPassphrase queue; the first is shown
	onRetry  func(hostID string)   // SetRetry
	pass     string                // the shown prompt's input
	passOpen bool

	// Shell state: the tab index into tabKeys (the last frame's visible
	// pages) for keyboard navigation, the content width the Processes page
	// reads, and whether the sidebar host switcher is expanded.
	tab          int
	tabKeys      []string
	split        float32 // content width
	hostMenuOpen bool

	// Chart window (an index into windows) and pause: while paused, views
	// are held back and now() stands still, so every page freezes.
	windowIdx int
	paused    bool
	clock     func() time.Time        // the running clock while paused
	held      map[string]hub.HostView // newest view per host received while paused
}

// NewApp seeds the per-host state from views(id) for each host. The first host
// is selected and the Overview page shown.
func NewApp(views func(id string) hub.HostView, hosts []store.Host, actions hub.Runner, onSelect func(hostID, page string)) *App {
	a := &App{hosts: slices.Clone(hosts), actions: actions, onSelect: onSelect, page: "overview",
		views: map[string]hub.HostView{}, held: map[string]hub.HostView{}, now: time.Now, windowIdx: 1,
		acts: pages.NewActions(actions), procs: pages.NewProcesses(), docker: pages.NewDocker(), apps: pages.NewApps(nil, nil), pkgs: pages.NewPackages(nil, nil)}
	pages.SetNavigate(a.ShowPage) // page links ("Show All") open their page
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

// SelectForShot sets the selected host and page directly, for the hwshot
// render tool. Not used by the running app.
func (a *App) SelectForShot(hostID, page string) {
	a.selected = hostID
	a.page = page
}

// Apply stores a copy of v for host id. Call it on the main thread.
func (a *App) Apply(id string, v hub.HostView) {
	if a.paused {
		a.held[id] = copyView(v) // shown on resume
		return
	}
	a.views[id] = copyView(v)
	a.gen++
}

// windows are the chart windows the page header offers, in order.
var (
	windows      = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	windowLabels = []string{"1m", "5m", "15m", "1h"}
)

// Window is how far back the charts look.
func (a *App) Window() time.Duration { return windows[min(max(a.windowIdx, 0), len(windows)-1)] }

// Paused reports whether updates are held back.
func (a *App) Paused() bool { return a.paused }

// togglePause freezes the window: views stop changing and now() stands
// still at the pause instant, so charts stop too. Resuming shows the newest
// view held for each host and lets the clock run again.
func (a *App) togglePause() {
	if a.paused {
		a.paused = false
		a.now = a.clock
		for id, v := range a.held {
			a.views[id] = v
			delete(a.held, id)
		}
		a.gen++
		return
	}
	a.paused = true
	a.clock = a.now
	frozen := a.clock()
	a.now = func() time.Time { return frozen }
}

// ApplyAction records an action snapshot published by the runner, keeping
// the highest Seq per action. Call it on the main thread.
func (a *App) ApplyAction(act hub.Action) { a.acts.Apply(act) }

// SetLogsOpener sets how the Docker page reaches container logs.
func (a *App) SetLogsOpener(o pages.LogsOpener) { a.docker.SetLogsOpener(o) }

// SetPost sets how the log reader hands work to the main thread, such as
// win.Update. Without one the work runs on the reader's goroutine.
func (a *App) SetPost(post func(func())) {
	a.post = post
	a.docker.SetPost(post)
}

// SetPackages installs the brew/mise manager behind the Packages page;
// call it after SetPost.
func (a *App) SetPackages(mgr pkgs.Manager) { a.pkgs = pages.NewPackages(mgr, a.post) }

// SetApps installs the application scanner behind the Apps page and how
// "Open" launches a bundle; call it after SetPost. nil leaves the page
// explaining that apps are listed for this Mac only.
func (a *App) SetApps(scanner apps.Scanner, open func(bundle string)) {
	a.apps = pages.NewApps(scanner, a.post)
	a.apps.Open = open
}

// SetColumnsHook sets the function called with a page's visible column ids,
// in order, when the user hides, shows or moves one; the caller persists them.
func (a *App) SetColumnsHook(f func(page string, cols []string)) { a.onColumns = f }

// SetSavedColumns restores a page's saved visible columns.
func (a *App) SetSavedColumns(page string, cols []string) {
	if page == "processes" {
		a.procs.SetColumns(cols)
	}
}

// SetSettings shows s on the Settings page and applies what the window
// itself honours (network units). Call it on the main thread.
func (a *App) SetSettings(s store.Settings) { a.settings = s }

// SetSettingsHook sets the function called with the new settings when the
// user changes one; the caller persists and applies them.
func (a *App) SetSettingsHook(f func(store.Settings)) { a.onSettings = f }

// SetIconSource installs where the pages get application icons (main
// thread); nil leaves every application on its initials tile.
func (a *App) SetIconSource(src appicon.Source) { pages.SetIconSource(src) }

func (a *App) changeSettings(s store.Settings) {
	a.settings = s
	if a.onSettings != nil {
		a.onSettings(s)
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

// HostPanel owns host management: the add/edit/remove and host-key dialogs,
// and the per-host actions menu. The sidebar's host switcher lists the
// hosts and calls into it; RenderDialogs runs once per frame after the
// window content. selected is the host the pages show and view returns any
// host's latest view.
type HostPanel interface {
	RenderDialogs(c *ui.Context, selected string, view func(id string) hub.HostView)
	OpenAdd()
	OpenHostKey()
	HostMenu(m *ui.Menu, h store.Host)
	Err() string
}

// SetHostPanel installs p; without one the switcher only switches hosts.
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

// selectHost switches every page to host id. A page the host cannot show
// becomes Overview, so onSelect never sees an invalid page.
func (a *App) selectHost(id string) {
	if id == a.selected {
		return
	}
	a.docker.CloseLogs()
	a.selected = id
	if !slices.ContainsFunc(visiblePages(a.views[id].Info.Caps), func(p Page) bool { return p.Key == a.page }) {
		a.page = "overview"
	}
	a.tab = -1 // the top bar's index belongs to the previous host's pages
	a.onSelect(id, a.page)
}

// View builds the window content.
func (a *App) View(c *ui.Context) {
	start := a.now()
	v := a.views[a.selected]
	Shell(c, a, func() {
		switch a.page {
		case "overview":
			pages.Overview(c, v, a.now())
		case "cpu":
			pages.CPU(c, v, a.now())
		case "memory":
			pages.Memory(c, v, a.now())
		case "disk":
			pages.Disk(c, v, a.now())
		case "network":
			pages.Network(c, v, a.now())
		case "gpu":
			pages.GPU(c, v, a.now())
		case "energy":
			pages.Energy(c, v, a.now())
		case "processes":
			a.procs.Render(c, pages.ProcessesInput{HostID: a.selected, View: v, Gen: a.gen, Now: a.now(),
				Width: a.split - 6*components.Grid, Actions: a.acts})
		case "docker":
			a.docker.Render(c, pages.DockerInput{HostID: a.selected, View: v, Gen: a.gen, Now: a.now(), Actions: a.acts})
		case "about":
			pages.About(c, v, a.now())
		case "packages":
			a.pkgs.Render(c, pages.PackagesInput{HostID: a.selected, Now: a.now(), Actions: a.acts, Width: a.split - 6*components.Grid})
		case "apps":
			a.apps.Render(c, pages.AppsInput{HostID: a.selected, Now: a.now(), Actions: a.acts, Width: a.split - 6*components.Grid})
		case "settings":
			pages.Settings(c, &a.settingsUI, a.settings, a.changeSettings)
		default:
			components.EmptyState(c, "This page is not built yet.")
		}
	})
	a.passphraseDialog(c)
	if a.panel != nil {
		a.panel.RenderDialogs(c, a.selected, func(id string) hub.HostView { return a.views[id] })
	}
	if a.onFrame != nil {
		a.onFrame(start)
	}
}
