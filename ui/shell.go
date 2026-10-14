package ui

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// Page is one item of the top bar.
type Page struct {
	Key, Title string
	icon       *ui.SVG
	needs      func(model.Caps) bool // nil: every host has it
}

// Pages in top bar order (R23).
var Pages = []Page{
	{Key: "overview", Title: "Overview", icon: icons.Overview},
	{Key: "cpu", Title: "CPU", icon: icons.CPU},
	{Key: "memory", Title: "Memory", icon: icons.Memory},
	{Key: "disk", Title: "Disk", icon: icons.Disk},
	{Key: "network", Title: "Network", icon: icons.Network},
	{Key: "gpu", Title: "GPU", icon: icons.GPU, needs: func(c model.Caps) bool { return c.GPU }},
	{Key: "energy", Title: "Energy", icon: icons.Energy, needs: func(c model.Caps) bool { return c.Battery }},
	{Key: "processes", Title: "Processes", icon: icons.Processes},
	{Key: "docker", Title: "Docker", icon: icons.Docker, needs: func(c model.Caps) bool { return c.Docker }},
	{Key: "apps", Title: "Apps", icon: icons.Apps, needs: func(c model.Caps) bool { return c.Apps }},
	{Key: "packages", Title: "Packages", icon: icons.Packages, needs: func(c model.Caps) bool { return c.Packages }},
	{Key: "settings", Title: "Settings", icon: icons.Settings},
}

// visiblePages are the pages a host with caps can show.
func visiblePages(caps model.Caps) []Page {
	var out []Page
	for _, p := range Pages {
		if p.needs == nil || p.needs(caps) {
			out = append(out, p)
		}
	}
	return out
}

// DefaultPanelWidth is the host panel's width until the user drags it.
const DefaultPanelWidth = 260

// Shell builds the window every page shares: the top bar (pages left, the
// selected host and its connection right), the page title, a banner when
// the host is not online, the page's content and the host panel on the
// right, in a split the user resizes. Pages build only content.
func Shell(c *ui.Context, a *App, hostPanel func(c *ui.Context), content func()) {
	c.SetTheme(themeFor(c.Theme().Dark))
	t := c.Theme()
	// A host chosen by a shortcut last frame is selected before any view is read.
	if id := a.nextHost; id != "" {
		a.nextHost = ""
		a.selectHost(id)
	}
	v := a.views[a.selected]
	now := a.now()

	// The top bar's tab changed a.tab after the last frame's input.
	if a.tab >= 0 && a.tab < len(a.tabKeys) && a.tabKeys[a.tab] != a.page {
		a.setPage(a.tabKeys[a.tab])
	}
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
	if w != a.lastW {
		a.split, a.lastW = max(w-1-a.panelW, 0), w
	} else {
		a.panelW = w - 1 - a.split
	}

	ui.Column(c).Fill().FontFeatures("tnum").Children(func() {
		a.topBar(c, pages, v, now)
		ui.Split(c, &a.split, func() {
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).Padding(3 * components.Grid).Gap(2 * components.Grid).Children(func() {
					ui.Text(c, title).FontSize(22).FontWeight(700)
					a.noticeBanner(c)
					if kind, text, ok := banner(v.Conn, now); ok {
						ui.Row(c).Gap(components.Grid).AlignItems(ui.Center).Children(func() {
							components.Banner(c, kind, text).Grow(1)
							a.retryButton(c, v.Conn.State)
						})
					}
					content()
				})
			})
		}, func() {
			ui.Column(c).Fill().Background(t.Surface).Children(func() { hostPanel(c) })
		}).Grow(1)
	})
	if !v.Conn.LastOK.IsZero() {
		c.After(time.Second) // ages tick
	}
}

// Top bar text styles; topBarWidth measures with the same ones.
var (
	brandSpan = ui.Span{Text: "Hotwatch", Size: 15, Weight: 700}
	tabSpan   = ui.Span{Size: 13, Weight: 500}
	hostSpan  = ui.Span{Size: 13, Weight: 600}
)

// Room the top bar's parts take besides their text, in DIPs.
const (
	tabChrome  = 2*10 + 14 + 6 + 2 // padding, icon, gap, list gap
	hostChrome = 14 + 8 + 8 + 110  // icon, gaps, a badge
	barChrome  = 4*2*components.Grid + 2*components.Grid
)

// topBarWidth estimates how wide the top bar is with every tab labelled,
// without the brand and the update age.
func topBarWidth(c *ui.Context, pages []Page, host string) float32 {
	var w float32
	for _, p := range pages {
		s := tabSpan
		s.Text = p.Title
		tw, _ := c.MeasureText(0, s)
		w += tw + tabChrome
	}
	hs := hostSpan
	hs.Text = host
	hw, _ := c.MeasureText(0, hs)
	return w + hw + hostChrome + barChrome
}

// topBar shows the pages and the selected host. When the window is too
// narrow, the brand and the update age go first; then the tabs other than
// the open one show only their icon (named for assistive technology and in
// a tooltip).
func (a *App) topBar(c *ui.Context, pages []Page, v hub.HostView, now time.Time) {
	t := c.Theme()
	h, ok := a.host()
	updated := ""
	if v.Conn.State == hub.Online && !v.Conn.LastOK.IsZero() {
		updated = "Updated " + format.Ago(now.Sub(v.Conn.LastOK))
	}
	w, _ := c.Size()
	need := topBarWidth(c, pages, h.Name)
	brandW, _ := c.MeasureText(0, brandSpan)
	updW, _ := c.MeasureText(0, ui.Span{Text: updated, Size: 12})
	full := need+brandW+updW+2*2*components.Grid <= w
	compact := need > w
	ui.Row(c).Height(7*components.Grid).PaddingX(2*components.Grid).Gap(2*components.Grid).
		Background(t.Surface).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		if full {
			ui.Text(c, brandSpan.Text).FontSize(brandSpan.Size).FontWeight(brandSpan.Weight).TextColor(t.Accent).NoWrap()
		}
		tabs := ui.TabsBase(c, &a.tab, len(pages))
		tabs.List.Gap(2).Label("Pages").Children(func() {
			for i, p := range pages {
				tab := tabs.Tab(i).Gap(6).Padding(6, 10).Radius(components.Grid)
				col := t.TextMuted
				switch {
				case i == a.tab:
					tab.Background(t.Accent.Alpha(0.12))
					col = t.Accent
				case tab.Hovered():
					tab.Background(t.SurfaceHover)
					col = t.Text
				}
				labelled := !compact || i == a.tab
				if !labelled {
					tab.Label(p.Title).Tooltip(p.Title)
				}
				tab.Children(func() {
					ui.Icon(c, p.icon).FontSize(14).TextColor(col)
					if labelled {
						ui.Text(c, p.Title).FontSize(tabSpan.Size).FontWeight(tabSpan.Weight).TextColor(col).NoWrap()
					}
				})
			}
		})
		ui.Spacer(c)
		if !ok {
			return
		}
		ui.Row(c).Gap(components.Grid).Shrink(0).Children(func() {
			ui.Icon(c, icons.Host).FontSize(14).TextColor(t.TextMuted)
			ui.Text(c, h.Name).FontSize(hostSpan.Size).FontWeight(hostSpan.Weight).NoWrap()
			if v.Conn.State != "" {
				components.Badge(c, string(v.Conn.State))
			}
			if full && updated != "" {
				ui.Text(c, updated).FontSize(12).TextColor(t.TextMuted).NoWrap()
			}
		})
	})
}

// banner is the notice under the page title for a host that is not online;
// ages are computed from conn.LastOK at render time.
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
