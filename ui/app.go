// Package ui is hotwatch's native window content.
package ui

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/store"
	"hotwatch/ui/pages"
)

// App holds everything the view reads. It is touched only on the main thread:
// Apply is called inside win.Update.
type App struct {
	hosts    []store.Host
	actions  hub.Runner
	onSelect func(hostID, page string)

	selected string
	page     string
	views    map[string]hub.HostView
	now      func() time.Time
}

// NewApp seeds the per-host state from views(id) for each host. The first host
// is selected and the Overview page shown.
func NewApp(views func(id string) hub.HostView, hosts []store.Host, actions hub.Runner, onSelect func(hostID, page string)) *App {
	a := &App{hosts: slices.Clone(hosts), actions: actions, onSelect: onSelect, page: "overview",
		views: map[string]hub.HostView{}, now: time.Now}
	for _, h := range a.hosts {
		a.views[h.ID] = copyView(views(h.ID))
	}
	if len(a.hosts) > 0 {
		a.selected = a.hosts[0].ID
	}
	return a
}

// Apply stores a copy of v for host id. Call it on the main thread.
func (a *App) Apply(id string, v hub.HostView) { a.views[id] = copyView(v) }

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

func (a *App) host() (store.Host, bool) {
	for _, h := range a.hosts {
		if h.ID == a.selected {
			return h, true
		}
	}
	return store.Host{}, false
}

// View builds the window content.
func (a *App) View(c *ui.Context) {
	t := c.Theme()
	h, ok := a.host()
	v := a.views[a.selected]
	now := a.now()
	ui.Column(c).Fill().Padding(20).Gap(16).Children(func() {
		ui.Row(c).Gap(12).Children(func() {
			ui.Text(c, "Overview").FontSize(22).Bold()
			ui.Spacer(c)
			if ok {
				ui.Text(c, h.Name).Bold()
				if v.Conn.State != "" {
					ui.Text(c, string(v.Conn.State)).TextColor(t.TextMuted)
				}
				if !v.Conn.LastOK.IsZero() {
					ui.Textf(c, "Updated %s ago", now.Sub(v.Conn.LastOK).Round(time.Second)).TextColor(t.TextMuted)
					c.After(time.Second)
				}
			}
		})
		pages.Overview(c, v, now)
	})
}
