package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// Page is one row of the sidebar.
type Page struct {
	Key, Title string
	icon       *ui.SVG
	needs      func(model.Caps) bool // nil: every host has it
}

// group is a labelled block of sidebar rows.
type group struct {
	title string // "" for the top, label-less block
	pages []Page
}

// Sidebar groups, in order.
var groups = []group{
	{pages: []Page{
		{Key: "overview", Title: "Overview", icon: icons.Overview},
		{Key: "processes", Title: "Processes", icon: icons.Processes},
	}},
	{title: "Hardware", pages: []Page{
		{Key: "cpu", Title: "CPU", icon: icons.CPU},
		{Key: "memory", Title: "Memory", icon: icons.Memory},
		{Key: "gpu", Title: "GPU", icon: icons.GPU, needs: func(c model.Caps) bool { return c.GPU }},
		{Key: "disk", Title: "Disk", icon: icons.Disk},
		{Key: "network", Title: "Network", icon: icons.Network},
		{Key: "energy", Title: "Energy", icon: icons.Energy, needs: func(c model.Caps) bool { return c.Battery }},
	}},
	{title: "System", pages: []Page{
		{Key: "docker", Title: "Docker", icon: icons.Docker, needs: func(c model.Caps) bool { return c.Docker }},
		{Key: "apps", Title: "Apps", icon: icons.Apps, needs: func(c model.Caps) bool { return c.Apps }},
		{Key: "packages", Title: "Packages", icon: icons.Packages, needs: func(c model.Caps) bool { return c.Packages }},
		{Key: "about", Title: "About This Mac", icon: icons.Info, needs: func(c model.Caps) bool { return c.Apps }},
		{Key: "settings", Title: "Settings", icon: icons.Settings},
	}},
}

// Pages is every page in sidebar order (used by shortcuts and page lookup).
var Pages = func() []Page {
	var out []Page
	for _, g := range groups {
		out = append(out, g.pages...)
	}
	return out
}()

// visiblePages are the pages a host with caps can show, in order.
func visiblePages(caps model.Caps) []Page {
	var out []Page
	for _, p := range Pages {
		if p.needs == nil || p.needs(caps) {
			out = append(out, p)
		}
	}
	return out
}

// SidebarWidth is the fixed width of the left navigation.
const SidebarWidth = 200

// Shell builds the window every page shares: a left sidebar (navigation with
// live values and a host switcher) and the page's content on the right.
func Shell(c *ui.Context, a *App, content func()) {
	c.SetTheme(themeFor(c.Theme().Dark))
	t := c.Theme()
	if id := a.nextHost; id != "" {
		a.nextHost = ""
		a.selectHost(id)
	}
	v := a.views[a.selected]
	now := a.now()
	components.SetChartWindow(a.Window())
	format.SetBitRates(a.settings.NetUnits == "bits")

	// a.page is the single source of truth: sidebar rows and shortcuts both
	// call setPage; a.tab is derived below for anything that still reads it.
	pages := visiblePages(v.Info.Caps)
	a.tabKeys = a.tabKeys[:0]
	for _, p := range pages {
		a.tabKeys = append(a.tabKeys, p.Key)
	}
	if !slices.Contains(a.tabKeys, a.page) {
		a.setPage("overview")
	}
	a.shortcuts(c, pages)
	a.tab = slices.Index(a.tabKeys, a.page)
	title := pages[a.tab].Title

	w, _ := c.Size()
	a.split = max(w-SidebarWidth, 0) // content width, read by the Processes page

	ui.Row(c).Fill().AlignItems(ui.Stretch).FontFeatures("tnum").Children(func() {
		a.sidebar(c, v, now)
		ui.Column(c).Grow(1).Background(t.Background).Children(func() {
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).Padding(3 * components.Grid).Gap(2 * components.Grid).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(2 * components.Grid).Children(func() {
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							ui.Text(c, title).FontSize(22).FontWeight(700)
							if sub := pageSubtitle(a.page, v); sub != "" {
								ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).SingleLine()
							}
						})
						ui.Segmented(c, &a.windowIdx, windowLabels...)
						a.pauseButton(c)
					})
					a.noticeBanner(c)
					if kind, text, ok := banner(v.Conn, now); ok {
						ui.Row(c).Gap(components.Grid).AlignItems(ui.Center).Children(func() {
							components.Banner(c, kind, text).Grow(1)
							if v.Conn.State == hub.HostKeyUnknown && a.panel != nil {
								if ui.Button(c, "Verify host key…").Clicked() {
									a.panel.OpenHostKey()
								}
							}
							a.retryButton(c, v.Conn.State)
						})
					}
					content()
				})
			})
		})
	})
	if ageTick(v.Conn) {
		c.After(time.Second) // "last seen Ns ago" ticks
	}
}

// ageTick reports whether the window must redraw every second for an age
// to count up: only while the host is behind (stale, offline, …) and was
// seen once. An online host's view is redrawn by every poll anyway, so a
// per-second frame would only cost CPU.
func ageTick(conn hub.Conn) bool {
	return conn.State != hub.Online && !conn.LastOK.IsZero()
}

// sidebar draws the left navigation: the grouped page rows with live values,
// and the host switcher pinned to the bottom.
func (a *App) sidebar(c *ui.Context, v hub.HostView, now time.Time) {
	t := c.Theme()
	caps := v.Info.Caps
	ui.Column(c).Width(SidebarWidth).Background(t.Surface).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		// The hidden title bar's window controls sit over this strip, which
		// also drags the window; it is 28 high when there are no controls.
		bar := c.TitleBar()
		ui.Row(c).Height(max(bar.Height, 28)).Padding(0, bar.Right, 0, bar.Left).DragWindow()
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(components.Grid/2, components.Grid).Gap(components.Grid / 2).Children(func() {
				for gi, g := range groups {
					rows := make([]Page, 0, len(g.pages))
					for _, p := range g.pages {
						if p.needs == nil || p.needs(caps) {
							rows = append(rows, p)
						}
					}
					if len(rows) == 0 {
						continue
					}
					_ = gi
					if g.title != "" {
						ui.Text(c, g.title).FontSize(11).FontWeight(600).TextColor(t.TextMuted).
							Padding(2*components.Grid, components.Grid, components.Grid/2, components.Grid)
					}
					for _, p := range rows {
						a.navRow(c, p, v)
					}
				}
			})
		})
		a.hostSwitcher(c, v, now)
	})
}

// navRow is one sidebar page row: a colour-tiled icon, the title, and the
// metric's live value. The selected row is an accent pill.
func (a *App) navRow(c *ui.Context, p Page, v hub.HostView) {
	t := c.Theme()
	selected := p.Key == a.page
	row := ui.Row(c).Key("nav-"+p.Key).AlignItems(ui.Center).Gap(components.Grid+2).
		Padding(3, components.Grid).Radius(components.Grid)
	switch {
	case selected:
		row.Background(t.Accent)
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	if row.Clicked() {
		a.setPage(p.Key)
	}
	label, value := t.Text, t.TextMuted
	if selected {
		label, value = t.AccentText, t.AccentText.Alpha(0.85)
	}
	tileBg := t.SurfaceHover
	iconCol := t.TextMuted
	if col, ok := components.MetricColor(p.Key); ok {
		tileBg, iconCol = col, ui.Hex("#ffffff")
	}
	if selected {
		tileBg = t.AccentText.Alpha(0.22)
		iconCol = t.AccentText
	}
	row.Children(func() {
		components.IconTile(c, p.icon, tileBg, iconCol)
		ui.Text(c, p.Title).FontSize(13).FontWeight(500).TextColor(label).Grow(1).NoWrap()
		s := navValue(p.Key, v.Snap)
		if p.Key == "processes" && len(v.Procs) > 0 {
			s = fmt.Sprint(len(v.Procs))
		}
		if s != "" {
			ui.Text(c, s).FontSize(12).TextColor(value).NoWrap()
		}
	})
}

// navValue is the compact live value shown on a hardware row.
func navValue(page string, s *model.Snapshot) string {
	if s == nil {
		return ""
	}
	pct := func(v float64) string { return fmt.Sprintf("%.0f%%", v) }
	switch page {
	case "cpu":
		return pct(s.CPU.Total)
	case "memory":
		if s.Memory.Total == 0 {
			return ""
		}
		return pct(float64(s.Memory.Used) / float64(s.Memory.Total) * 100)
	case "gpu":
		if len(s.GPUs) > 0 && s.GPUs[0].UtilPct != nil {
			return pct(*s.GPUs[0].UtilPct)
		}
	case "disk":
		var sum float64
		ok := false
		for _, d := range model.DistinctVolumes(s.Disks) {
			for _, r := range []*float64{d.ReadBps, d.WriteBps} {
				if r != nil {
					sum += *r
					ok = true
				}
			}
		}
		if ok {
			return format.Rate(&sum)
		}
	case "network":
		var sum float64
		ok := false
		for _, i := range s.Net {
			if i.Name == "lo" || i.Name == "lo0" || !i.Up {
				continue
			}
			for _, r := range []*float64{i.RxBps, i.TxBps} {
				if r != nil {
					sum += *r
					ok = true
				}
			}
		}
		if ok {
			return format.Rate(&sum)
		}
	case "energy":
		if s.Battery != nil {
			return pct(s.Battery.ChargePct)
		}
	}
	return ""
}

// hostSwitcher is the bottom block: the current host, and on click an inline
// list of the other hosts to switch to.
func (a *App) hostSwitcher(c *ui.Context, v hub.HostView, now time.Time) {
	t := c.Theme()
	cur, _ := a.host()
	ui.Column(c).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Padding(components.Grid).Gap(2).Children(func() {
		if a.hostMenuOpen {
			for _, h := range a.hosts {
				if h.ID == a.selected {
					continue
				}
				item := ui.Row(c).Key("hs-"+h.ID).AlignItems(ui.Center).Gap(components.Grid).
					Padding(6, components.Grid).Radius(components.Grid)
				if item.Hovered() {
					item.Background(t.SurfaceHover)
				}
				if item.Clicked() {
					a.selectHost(h.ID)
					a.hostMenuOpen = false
				}
				if a.panel != nil {
					item.ContextMenu(func(m *ui.Menu) { a.panel.HostMenu(m, h) })
				}
				item.Children(func() {
					ui.Box(c).Size(8, 8).Radius(4).Background(components.StateColor(t, string(a.views[h.ID].Conn.State)))
					ui.Text(c, h.Name).FontSize(13).SingleLine().Grow(1)
				})
			}
			if a.panel != nil {
				add := ui.Row(c).Key("hs-add").AlignItems(ui.Center).Gap(components.Grid).
					Padding(6, components.Grid).Radius(components.Grid)
				if add.Hovered() {
					add.Background(t.SurfaceHover)
				}
				if add.Clicked() {
					a.hostMenuOpen = false
					a.panel.OpenAdd()
				}
				add.Children(func() {
					ui.Text(c, "+").FontSize(13).FontWeight(600).TextColor(t.Accent).Width(8)
					ui.Text(c, "Add host…").FontSize(13).TextColor(t.Accent).SingleLine().Grow(1)
				})
			}
		}
		if a.panel != nil {
			if e := a.panel.Err(); e != "" {
				ui.Text(c, e).FontSize(11).TextColor(t.Danger).Padding(2, components.Grid)
			}
		}
		btn := ui.Row(c).Key("host-switch").AlignItems(ui.Center).Gap(components.Grid).
			Padding(6, components.Grid).Radius(components.Grid)
		if btn.Hovered() {
			btn.Background(t.SurfaceHover)
		}
		if btn.Clicked() {
			a.hostMenuOpen = !a.hostMenuOpen
		}
		if a.panel != nil {
			btn.ContextMenu(func(m *ui.Menu) { a.panel.HostMenu(m, cur) })
		}
		btn.Children(func() {
			ui.Box(c).Size(10, 10).Radius(5).Background(components.StateColor(t, string(v.Conn.State)))
			ui.Column(c).Grow(1).Children(func() {
				ui.Text(c, cur.Name).FontSize(13).FontWeight(600).SingleLine()
				ui.Text(c, hostSubtitle(cur, v, now)).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
			ui.Icon(c, icons.ChevronDown).FontSize(14).TextColor(t.TextMuted)
		})
	})
}

// hostSubtitle is the small line under the host name: uptime and the age of
// the last update when online, else the connection state.
func hostSubtitle(h store.Host, v hub.HostView, now time.Time) string {
	if v.Conn.State != hub.Online {
		if v.Conn.State == "" {
			return "—"
		}
		return string(v.Conn.State)
	}
	if v.Snap != nil && v.Snap.Uptime > 0 {
		s := "Up " + format.Duration(v.Snap.Uptime)
		if !v.Conn.LastOK.IsZero() {
			s += " · " + format.Ago(now.Sub(v.Conn.LastOK))
		}
		return s
	}
	if v.Info.OS != "" {
		return v.Info.OS
	}
	return "Online"
}

// banner is the notice under the page title for a host that is not online.
func banner(conn hub.Conn, now time.Time) (components.BannerKind, string, bool) {
	age := func(prefix string) string {
		if conn.LastOK.IsZero() {
			return "never connected"
		}
		return prefix + format.Ago(now.Sub(conn.LastOK))
	}
	switch conn.State {
	case hub.Offline:
		return components.BannerError, "Offline · " + age("last seen "), true
	case hub.Stale:
		return components.BannerWarning, "Stale · " + age("updated "), true
	case hub.Unauthorized:
		return components.BannerError, "Not authorized · check the host's token or SSH login", true
	case hub.Incompatible:
		return components.BannerError, "Incompatible agent · update hotwatch-agent on this host", true
	case hub.HostKeyUnknown:
		return components.BannerWarning, "Verify host key · confirm its fingerprint to connect", true
	case hub.HostKeyMismatch:
		return components.BannerError, "Host key changed · possible attack; remove the old key from known_hosts by hand", true
	}
	return 0, "", false
}

// pageSubtitle is the muted line under the page title: a figure about the
// page's subject when the host has one.
func pageSubtitle(page string, v hub.HostView) string {
	s := v.Snap
	switch page {
	case "overview":
		return v.Info.Hostname
	case "processes":
		if n := len(v.Procs); n > 0 {
			threads := 0
			for _, p := range v.Procs {
				threads += int(p.Threads)
			}
			sub := plural(n, "process")
			if threads > 0 {
				sub += " · " + plural(threads, "thread")
			}
			return sub
		}
	case "cpu":
		m := v.Info.Machine
		if m.PerfCores > 0 && m.EffCores > 0 {
			sub := fmt.Sprintf("%d Performance + %d Efficiency cores", m.PerfCores, m.EffCores)
			if m.Chip != "" {
				sub = m.Chip + " · " + sub
			}
			return sub
		}
		if s != nil && len(s.CPU.PerCore) > 0 {
			return plural(len(s.CPU.PerCore), "core")
		}
	case "memory":
		if s != nil && s.Memory.Total > 0 {
			return "In use of " + format.Bytes(s.Memory.Total)
		}
	case "gpu":
		if s != nil && len(s.GPUs) > 0 {
			return s.GPUs[0].Name
		}
	case "disk":
		if s != nil && len(s.Disks) > 0 {
			return plural(len(s.Disks), "volume")
		}
	case "network":
		if s != nil {
			up := 0
			for _, i := range s.Net {
				if i.Up && i.Name != "lo" && i.Name != "lo0" {
					up++
				}
			}
			if up > 0 {
				return plural(up, "interface") + " up"
			}
		}
	case "energy":
		return "Battery and power"
	case "about":
		return v.Info.Machine.Model
	case "docker":
		if n := len(v.Ctrs); n > 0 {
			return plural(n, "container")
		}
	}
	return ""
}

// plural counts noun: "1 core", "8 cores", "2 processes".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	if strings.HasSuffix(noun, "s") {
		return fmt.Sprintf("%d %ses", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// pauseButton toggles holding updates back; it shows a play glyph and a
// warning tint while paused.
func (a *App) pauseButton(c *ui.Context) {
	t := c.Theme()
	icon, tip := icons.Pause, "Pause updates"
	if a.paused {
		icon, tip = icons.Play, "Resume updates"
	}
	b := ui.Row(c).Key("pause").Size(28, 28).Radius(components.Grid).Center().Label(tip).Tooltip(tip)
	switch {
	case a.paused:
		b.Background(t.Warning.Alpha(0.16))
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
	if b.Clicked() {
		a.togglePause()
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(t.Text)
	})
}
