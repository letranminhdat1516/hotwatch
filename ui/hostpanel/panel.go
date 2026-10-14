// Package hostpanel is the right-hand host panel: the grouped host tree,
// the add/edit host dialog, group management and the SSH host-key trust
// dialog. It never touches disk, the keychain or the hub itself; every side
// effect goes through Effects.
package hostpanel

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/store"
	"hotwatch/transport"
	"hotwatch/ui/components"
)

// Effects is what the panel asks the application to do. The panel owns the
// Config it edits and passes every changed copy to SaveConfig; it adopts the
// copy only when SaveConfig succeeds.
type Effects interface {
	SaveConfig(store.Config) error
	SetToken(hostID, token string) error
	// TrustHostKey records hk.Key (never a re-fetched key) after the user
	// approved hk.Fingerprint, then retries the host.
	TrustHostKey(hostID string, hk *transport.HostKeyError) error
	HostAdded(store.Host)
	HostChanged(store.Host)
	HostRemoved(id string)
	Select(hostID string)
}

// Panel is the host panel's state. Use it on the main thread only.
type Panel struct {
	cfg store.Config
	eff Effects

	err     string // last failed save or token write, shown under the tree
	pending func() // change chosen in a menu, applied after the tree is built

	form     hostForm
	group    groupDialog
	removing struct {
		open     bool
		id, name string
	}
	hk hostKeyState
}

// New returns a panel editing cfg.
func New(cfg store.Config, eff Effects) *Panel {
	return &Panel{cfg: cloneConfig(cfg), eff: eff}
}

// Config is the panel's current configuration.
func (p *Panel) Config() store.Config { return cloneConfig(p.cfg) }

// SetConfig replaces the configuration after a change made elsewhere.
func (p *Panel) SetConfig(cfg store.Config) { p.cfg = cloneConfig(cfg) }

func cloneConfig(c store.Config) store.Config {
	c.Groups = slices.Clone(c.Groups)
	c.Hosts = slices.Clone(c.Hosts)
	c.UI.OpenGroups = slices.Clone(c.UI.OpenGroups)
	return c
}

// edit applies change to a copy of the configuration and adopts the copy if
// it saves.
func (p *Panel) edit(change func(*store.Config) error) error {
	next := cloneConfig(p.cfg)
	if err := change(&next); err != nil {
		p.err = err.Error()
		return err
	}
	if err := p.eff.SaveConfig(next); err != nil {
		p.err = "Could not save the configuration: " + err.Error()
		return err
	}
	p.cfg, p.err = next, ""
	return nil
}

func newID(prefix string) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // the system's random source failing is not recoverable
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func (p *Panel) isOpen(groupID string) bool { return slices.Contains(p.cfg.UI.OpenGroups, groupID) }

func (p *Panel) toggle(groupID string) {
	_ = p.edit(func(c *store.Config) error {
		if i := slices.Index(c.UI.OpenGroups, groupID); i >= 0 {
			c.UI.OpenGroups = slices.Delete(c.UI.OpenGroups, i, i+1)
		} else {
			c.UI.OpenGroups = append(c.UI.OpenGroups, groupID)
		}
		return nil
	})
}

// Render builds the panel. selected is the host the pages show; view returns
// a host's latest view.
func (p *Panel) Render(c *ui.Context, selected string, view func(id string) hub.HostView) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(components.Grid, components.Grid, components.Grid, 2*components.Grid).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Hosts").FontSize(12).FontWeight(600).TextColor(t.TextMuted).Grow(1)
			ui.Button(c, "+").Tooltip("Add a host or a group").Menu(func(m *ui.Menu) {
				if m.Item("Add host…").Chosen() {
					p.pending = p.form.openAdd
				}
				if m.Item("New group…").Chosen() {
					p.pending = func() { p.group.open("") }
				}
			})
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(0, components.Grid, components.Grid, components.Grid).Gap(2).Children(func() {
				p.tree(c, selected, view)
			})
		})
		p.footer(c, selected, view)
	})
	p.form.dialog(c, p)
	p.group.dialog(c, p)
	p.removeDialog(c)
	p.hostKeyDialog(c, selected, view)
	if f := p.pending; f != nil {
		p.pending = nil
		f()
	}
}

func (p *Panel) footer(c *ui.Context, selected string, view func(string) hub.HostView) {
	if p.err == "" && !p.hostKeyNote(c, selected, view, false) {
		return
	}
	t := c.Theme()
	ui.Column(c).Padding(components.Grid, 2*components.Grid).Gap(components.Grid).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		if p.err != "" {
			ui.Text(c, p.err).FontSize(12).TextColor(t.Danger)
		}
		p.hostKeyNote(c, selected, view, true)
	})
}

// tree builds the groups and hosts under the root.
func (p *Panel) tree(c *ui.Context, selected string, view func(string) hub.HostView) {
	known := map[string]bool{}
	for _, g := range p.cfg.Groups {
		known[g.ID] = true
	}
	// A group or host naming a parent that does not exist shows at the top
	// level rather than disappearing.
	parentOf := func(id string) string {
		if known[id] {
			return id
		}
		return ""
	}
	seen := map[string]bool{}
	var level func(parent string, depth int)
	level = func(parent string, depth int) {
		for _, g := range p.cfg.Groups {
			if parentOf(g.Parent) != parent || g.ID == parent || seen[g.ID] {
				continue
			}
			seen[g.ID] = true
			p.groupRow(c, g, depth)
			if p.isOpen(g.ID) {
				level(g.ID, depth+1)
			}
		}
		for _, h := range p.cfg.Hosts {
			if parentOf(h.Group) == parent {
				p.hostRow(c, h, depth, h.ID == selected, view(h.ID))
			}
		}
	}
	level("", 0)
}

func indent(depth int) float32 { return float32(depth)*14 + components.Grid }

func (p *Panel) groupRow(c *ui.Context, g store.Group, depth int) {
	t := c.Theme()
	row := ui.Row(c).Key("g:"+g.ID).Height(28).Gap(6).AlignItems(ui.Center).Padding(0, components.Grid, 0, indent(depth)).Radius(components.Grid)
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	if row.Clicked() {
		p.pending = func() { p.toggle(g.ID) }
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Rename…").Chosen() {
			p.pending = func() { p.group.rename(g) }
		}
		if m.Item("New subgroup…").Chosen() {
			p.pending = func() { p.group.open(g.ID) }
		}
		m.Separator()
		if m.Item("Remove group").Chosen() {
			p.pending = func() { _ = p.edit(func(c *store.Config) error { c.RemoveGroup(g.ID); return nil }) }
		}
	})
	chevron := "▸"
	if p.isOpen(g.ID) {
		chevron = "▾"
	}
	row.Children(func() {
		ui.Text(c, chevron).FontSize(10).TextColor(t.TextMuted).NoWrap()
		ui.Text(c, g.Name).FontSize(13).FontWeight(600).SingleLine().Grow(1)
	})
}

func (p *Panel) hostRow(c *ui.Context, h store.Host, depth int, selected bool, v hub.HostView) {
	t := c.Theme()
	row := ui.Row(c).Key("h:"+h.ID).Gap(components.Grid+2).AlignItems(ui.Center).Padding(5, components.Grid, 5, indent(depth)+14).Radius(components.Grid)
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
		ui.Box(c).Size(8, 8).Radius(4).Shrink(0).Background(dotColor(t, v.Conn.State))
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, h.Name).FontSize(13).SingleLine()
			ui.Text(c, readout(v)).FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
	})
}

func (p *Panel) hostMenu(m *ui.Menu, h store.Host) {
	if m.Item("Edit…").Chosen() {
		p.pending = func() { p.form.openEdit(h) }
	}
	m.Submenu("Move to group", func(m *ui.Menu) {
		if m.Item("No group").Checked(h.Group == "").Chosen() {
			p.pending = func() { p.move(h.ID, "") }
		}
		for _, g := range p.cfg.Groups {
			if m.Item(p.groupPath(g)).Checked(h.Group == g.ID).Chosen() {
				p.pending = func() { p.move(h.ID, g.ID) }
			}
		}
	})
	m.Separator()
	if m.Item("Remove…").Disabled(h.Kind == "local").Chosen() {
		p.pending = func() { p.removing.open, p.removing.id, p.removing.name = true, h.ID, h.Name }
	}
}

// groupPath names a group with its ancestors: "Servers / Web".
func (p *Panel) groupPath(g store.Group) string {
	name := g.Name
	for depth := 0; g.Parent != "" && depth < len(p.cfg.Groups); depth++ {
		i := slices.IndexFunc(p.cfg.Groups, func(x store.Group) bool { return x.ID == g.Parent })
		if i < 0 {
			break
		}
		g = p.cfg.Groups[i]
		name = g.Name + " / " + name
	}
	return name
}

func (p *Panel) move(hostID, groupID string) {
	_ = p.edit(func(c *store.Config) error { return c.MoveHost(hostID, groupID) })
}

func (p *Panel) removeDialog(c *ui.Context) {
	if !p.removing.open {
		return
	}
	id := p.removing.id
	msg := "Its token is deleted and it is no longer monitored."
	if ui.AlertDialog(c, &p.removing.open, fmt.Sprintf("Remove “%s”?", p.removing.name), msg, "Cancel", "Remove") != 1 {
		return
	}
	if p.edit(func(c *store.Config) error {
		c.Hosts = slices.DeleteFunc(c.Hosts, func(h store.Host) bool { return h.ID == id })
		return nil
	}) == nil {
		p.eff.HostRemoved(id)
	}
}

func dotColor(t *ui.Theme, s hub.ConnState) ui.Color {
	switch s {
	case hub.Online:
		return t.Success
	case hub.Stale, hub.Connecting, hub.HostKeyUnknown:
		return t.Warning
	case hub.Offline, hub.Unauthorized, hub.Incompatible, hub.HostKeyMismatch:
		return t.Danger
	}
	return t.TextMuted
}

// readout is the row's "CPU 12% · RAM 43%"; unknown values are dashes.
func readout(v hub.HostView) string {
	cpu, ram := "—", "—"
	if v.Snap != nil {
		cpu = fmt.Sprintf("%.0f%%", v.Snap.CPU.Total)
		if m := v.Snap.Memory; m.Total > 0 {
			ram = fmt.Sprintf("%.0f%%", float64(m.Used)/float64(m.Total)*100)
		}
	}
	return "CPU " + cpu + " · RAM " + ram
}
