package ui

import (
	"github.com/egoist/mygo/ui"
)

// pageKeys are the keys of the first nine visible pages: Cmd+1 to Cmd+9.
var pageKeys = [...]ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// hostOrderer is a host panel that knows the order it lists hosts in.
type hostOrderer interface{ HostOrder() []string }

// shortcuts applies this frame's keyboard navigation (R23): Cmd+1..9 open
// the selected host's visible pages in the top bar's order, Cmd+, opens
// Settings, Alt+Cmd+Down and Up select the next and previous host in the
// panel's order, round the ends, and Cmd+F focuses the Processes search.
// pages are the keys of the pages visible for the selected host.
func (a *App) shortcuts(c *ui.Context, pages []Page) {
	for i, k := range pageKeys {
		if c.Shortcut(ui.Cmd, k) && i < len(pages) {
			a.setPage(pages[i].Key)
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.setPage("settings")
	}
	if c.Shortcut(ui.Cmd|ui.Alt, ui.KeyDown) {
		a.stepHost(1)
	}
	if c.Shortcut(ui.Cmd|ui.Alt, ui.KeyUp) {
		a.stepHost(-1)
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) && a.page == "processes" {
		a.procs.FocusSearch()
	}
}

// hostOrder is the order hosts are listed in: the panel's, else the config's.
func (a *App) hostOrder() []string {
	if o, ok := a.panel.(hostOrderer); ok {
		return o.HostOrder()
	}
	ids := make([]string, len(a.hosts))
	for i, h := range a.hosts {
		ids[i] = h.ID
	}
	return ids
}

// stepHost selects the host delta places after the selected one, wrapping.
func (a *App) stepHost(delta int) {
	order := a.hostOrder()
	if len(order) == 0 {
		return
	}
	i := 0
	for j, id := range order {
		if id == a.selected {
			i = j
		}
	}
	a.selectHost(order[((i+delta)%len(order)+len(order))%len(order)])
}
