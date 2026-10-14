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
		a.stepHost(c, 1)
	}
	if c.Shortcut(ui.Cmd|ui.Alt, ui.KeyUp) {
		a.stepHost(c, -1)
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

// stepHost chooses the host delta places after the selected one (or after
// the one already chosen this frame), wrapping. A selected host missing from
// the order counts as index 0. The switch happens at the start of the next
// frame so no frame pairs one host's id with another's data.
func (a *App) stepHost(c *ui.Context, delta int) {
	order := a.hostOrder()
	if len(order) == 0 {
		return
	}
	from := a.selected
	if a.nextHost != "" {
		from = a.nextHost
	}
	i := 0
	for j, id := range order {
		if id == from {
			i = j
		}
	}
	a.nextHost = order[((i+delta)%len(order)+len(order))%len(order)]
	c.Invalidate()
}
