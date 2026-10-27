package hostpanel

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/store"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// section is one group of the Hosts page: its heading and the hosts it
// holds directly. group is nil for the hosts in no group.
type section struct {
	group *store.Group
	hosts []store.Host
}

// sections lists the groups in tree order, each with its own hosts, then
// the hosts in no group (or naming a group that does not exist).
func (p *Panel) sections() []section {
	var out []section
	idx := map[string]int{}
	p.walk(true, func(g store.Group, _ int) {
		idx[g.ID] = len(out)
		out = append(out, section{group: &g})
	}, func(h store.Host, _ int) {
		i, ok := idx[h.Group]
		if !ok {
			i, ok = idx[""]
			if !ok {
				i = len(out)
				idx[""] = i
				out = append(out, section{})
			}
		}
		out[i].hosts = append(out[i].hosts, h)
	})
	// walk lists a level's groups before its hosts, so "No group" can sit
	// between groups; it reads better last.
	if i, ok := idx[""]; ok && i != len(out)-1 {
		s := out[i]
		out = append(slices.Delete(out, i, i+1), s)
	}
	return out
}

// connLine is how a host is reached, as the Hosts page shows it.
func connLine(h store.Host) string {
	c := h.Conn
	switch {
	case h.Kind == "local":
		return "This computer"
	case c == nil:
		return "No connection set"
	case c.Type == "ssh":
		at := c.SSHHost
		if c.SSHUser != "" {
			at = c.SSHUser + "@" + at
		}
		return fmt.Sprintf("SSH · %s:%d", at, c.SSHPort)
	}
	return fmt.Sprintf("Tailscale / LAN · %s:%d", c.Address, c.AgentPort)
}

func (p *Panel) matches(h store.Host) bool {
	q := strings.ToLower(strings.TrimSpace(p.query))
	return q == "" || strings.Contains(strings.ToLower(h.Name+" "+connLine(h)), q)
}

// Manage builds the Hosts page: every host by group with its connection
// and state, buttons to add, edit, move and remove hosts and to manage
// groups, and the selected host's connection log. Its dialogs are drawn by
// RenderDialogs (or Render).
func (p *Panel) Manage(c *ui.Context, selected string, view func(id string) hub.HostView, now time.Time) {
	t := c.Theme()
	ui.Column(c).Gap(2 * components.Grid).Children(func() {
		ui.Row(c).Gap(components.Grid).AlignItems(ui.Center).Children(func() {
			ui.SearchField(c, &p.query).Label("Search hosts").Placeholder("Search hosts").Width(260)
			ui.Box(c).Grow(1)
			if ui.Button(c, "New group").Clicked() {
				p.pending = func() { p.group.open("") }
			}
			if ui.PrimaryButton(c, "New host").Clicked() {
				p.pending = p.form.openAdd
			}
		})
		if p.err != "" {
			ui.Text(c, p.err).FontSize(12).TextColor(t.Danger)
		}
		shown := 0
		for _, s := range p.sections() {
			hosts := slices.DeleteFunc(slices.Clone(s.hosts), func(h store.Host) bool { return !p.matches(h) })
			if p.query != "" && len(hosts) == 0 {
				continue
			}
			shown += len(hosts)
			p.manageSection(c, s.group, hosts, selected, view, now)
		}
		if shown == 0 && p.query != "" {
			components.EmptyState(c, "No host matches “"+strings.TrimSpace(p.query)+"”.")
		}
		p.connLog(c, selected, view(selected), now)
	})
}

func (p *Panel) manageSection(c *ui.Context, g *store.Group, hosts []store.Host, selected string, view func(string) hub.HostView, now time.Time) {
	t := c.Theme()
	title, key := "No group", "ms:"
	if g != nil {
		title, key = p.groupPath(*g), "ms:"+g.ID
	}
	ui.Column(c).Key(key).Gap(components.Grid).Children(func() {
		ui.Row(c).Gap(components.Grid).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(700).TextColor(t.TextMuted).Grow(1).SingleLine()
			if g != nil {
				grp := *g
				ui.MenuButton(c, "•••", func(m *ui.Menu) {
					if m.Item("Rename…").Chosen() {
						p.pending = func() { p.group.rename(grp) }
					}
					if m.Item("New subgroup…").Chosen() {
						p.pending = func() { p.group.open(grp.ID) }
					}
					m.Separator()
					if m.Item("Remove group").Chosen() {
						p.pending = func() { _ = p.edit(func(c *store.Config) error { c.RemoveGroup(grp.ID); return nil }) }
					}
				}).Label("Group actions " + grp.Name).Tooltip("Rename, add a subgroup or remove “" + grp.Name + "”")
			}
		})
		if len(hosts) == 0 {
			ui.Text(c, "No hosts in this group.").FontSize(12).TextColor(t.TextMuted)
			return
		}
		components.Surface(c, ui.Column(c).Gap(0).Padding(components.Grid/2)).Children(func() {
			for _, h := range hosts {
				p.manageRow(c, h, h.ID == selected, view(h.ID), now)
			}
		})
	})
}

func (p *Panel) manageRow(c *ui.Context, h store.Host, selected bool, v hub.HostView, now time.Time) {
	t := c.Theme()
	row := ui.Row(c).Key("m:"+h.ID).Gap(components.Grid+4).AlignItems(ui.Center).
		Padding(components.Grid, 1.5*components.Grid).Radius(components.Grid)
	switch {
	case selected:
		row.Background(t.Accent.Alpha(0.12))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	if row.Clicked() {
		p.pending = func() { p.eff.Select(h.ID) }
	}
	row.ContextMenu(func(m *ui.Menu) { p.hostMenu(m, h) })
	row.Children(func() {
		ui.Box(c).Size(10, 10).Radius(5).Shrink(0).Background(dotColor(t, v.Conn.State))
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, h.Name).FontSize(14).FontWeight(600).SingleLine()
			ui.Text(c, connLine(h)).FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
		if s := lastSeen(v.Conn, now); s != "" {
			ui.Text(c, s).FontSize(12).TextColor(t.TextMuted).NoWrap()
		}
		if v.Conn.State != "" {
			components.Badge(c, string(v.Conn.State))
		}
		if ui.Button(c, "Edit…").Label("Edit " + h.Name).Clicked() {
			p.pending = func() { p.form.openEdit(h) }
		}
		ui.MenuButton(c, "Move", func(m *ui.Menu) { p.moveMenu(m, h) }).Label("Move " + h.Name).Tooltip("Move to a group")
		if h.Kind != "local" {
			if ui.Button(c, "Remove…").Label("Remove " + h.Name).Clicked() {
				p.pending = func() { p.askRemove(h) }
			}
		}
	})
}

// lastSeen is "Last seen 3m ago" for a host that is not online but was once.
func lastSeen(conn hub.Conn, now time.Time) string {
	if conn.State == hub.Online || conn.LastOK.IsZero() {
		return ""
	}
	return "Last seen " + format.Ago(now.Sub(conn.LastOK))
}

// connLog lists the selected host's connection events, newest first.
func (p *Panel) connLog(c *ui.Context, selected string, v hub.HostView, now time.Time) {
	t := c.Theme()
	i := slices.IndexFunc(p.cfg.Hosts, func(h store.Host) bool { return h.ID == selected })
	if i < 0 {
		return
	}
	ui.Column(c).Gap(components.Grid).Padding(components.Grid, 0, 0, 0).Children(func() {
		ui.Text(c, "Connection log · "+p.cfg.Hosts[i].Name).FontSize(16).FontWeight(700)
		if len(v.Log) == 0 {
			components.EmptyState(c, "No connection events yet.")
			return
		}
		components.Surface(c, ui.Column(c).Gap(components.Grid)).Children(func() {
			for j := len(v.Log) - 1; j >= 0; j-- {
				e := v.Log[j]
				ui.Row(c).Key(j).Gap(components.Grid + 4).AlignItems(ui.Center).Children(func() {
					ui.Text(c, logTime(e.Time, now)).FontSize(12).Font("monospace").TextColor(t.TextMuted).NoWrap()
					components.Badge(c, string(e.State))
					ui.Text(c, e.Detail).FontSize(12).Grow(1)
				})
			}
		})
	})
}

// logTime is the clock time of an entry, with the date when it is not today.
func logTime(at, now time.Time) string {
	if y, m, d := at.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return at.Format("15:04:05")
	}
	return at.Format("Jan 2 15:04:05")
}
