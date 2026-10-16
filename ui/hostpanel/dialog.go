package hostpanel

import (
	"fmt"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"hotwatch/store"
)

const (
	defaultAgentPort = "7171"
	defaultSSHPort   = "22"
	minTokenLen      = 32 // agent tokens are 43 base64url characters
)

const (
	connDirect = iota
	connSSH
)

// hostForm is the add/edit host dialog.
type hostForm struct {
	isOpen    bool
	editID    string // "" when adding
	local     bool   // This Mac: only the name can change
	submitted bool   // show validation messages
	saveErr   string

	name, token                 string
	conn                        int
	addr, port                  string // direct
	sshHost, sshPort, user, key string // ssh
	agentPort                   string // ssh: the agent's port on the server
}

func (f *hostForm) reset() {
	*f = hostForm{isOpen: true, port: defaultAgentPort, sshPort: defaultSSHPort, agentPort: defaultAgentPort, user: currentUser()}
}

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

func (f *hostForm) openAdd() { f.reset() }

func (f *hostForm) openEdit(h store.Host) {
	f.reset()
	f.editID, f.local, f.name = h.ID, h.Kind == "local", h.Name
	if c := h.Conn; c != nil {
		switch c.Type {
		case "ssh":
			f.conn = connSSH
			f.sshHost, f.sshPort, f.user, f.key = c.SSHHost, strconv.Itoa(c.SSHPort), c.SSHUser, c.KeyPath
			f.agentPort = strconv.Itoa(c.AgentPort)
		default:
			f.addr, f.port = c.Address, strconv.Itoa(c.AgentPort)
		}
	}
}

type formErrors struct{ name, addr, port, sshHost, sshPort, agentPort, token string }

func (e formErrors) any() bool { return e != formErrors{} }

const portMsg = "Enter a port from 1 to 65535."

func validPort(s string) bool {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil && n >= 1 && n <= 65535
}

func (f *hostForm) validate() formErrors {
	var e formErrors
	if strings.TrimSpace(f.name) == "" {
		e.name = "Enter a name."
	}
	if f.local {
		return e
	}
	if f.conn == connDirect {
		if strings.TrimSpace(f.addr) == "" {
			e.addr = "Enter an address."
		}
		if !validPort(f.port) {
			e.port = portMsg
		}
	} else {
		if strings.TrimSpace(f.sshHost) == "" {
			e.sshHost = "Enter the SSH host."
		}
		if !validPort(f.sshPort) {
			e.sshPort = portMsg
		}
		if !validPort(f.agentPort) {
			e.agentPort = portMsg
		}
	}
	tok := strings.TrimSpace(f.token)
	if (f.editID == "" || tok != "") && len(tok) < minTokenLen {
		e.token = fmt.Sprintf("The token must be at least %d characters.", minTokenLen)
	}
	return e
}

func port(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

// host builds the host the form describes on top of base (the host being
// edited, or zero when adding).
func (f *hostForm) host(base store.Host) store.Host {
	h := base
	h.Name = strings.TrimSpace(f.name)
	if f.local {
		return h
	}
	h.Kind = "agent"
	if f.conn == connDirect {
		h.Conn = &store.Conn{Type: "direct", Address: strings.TrimSpace(f.addr), AgentPort: port(f.port)}
	} else {
		h.Conn = &store.Conn{Type: "ssh", SSHHost: strings.TrimSpace(f.sshHost), SSHPort: port(f.sshPort),
			SSHUser: strings.TrimSpace(f.user), KeyPath: strings.TrimSpace(f.key), AgentPort: port(f.agentPort)}
	}
	return h
}

func (f *hostForm) dialog(c *ui.Context, p *Panel) {
	if !f.isOpen {
		return
	}
	title, action := "New host", "Add host"
	if f.editID != "" {
		title, action = "Edit host", "Save"
	}
	t := c.Theme()
	// The modal builds its content here, so its buttons act
	// on the panel as it builds.
	ui.Modal(c, &f.isOpen, func() {
		errs := formErrors{}
		if f.submitted {
			errs = f.validate()
		}
		ui.Column(c).Width(420).Gap(16).Children(func() {
			ui.Text(c, title).FontSize(16).FontWeight(700)
			ui.Form(c, func() {
				ui.Field(c, "Name", func() { ui.TextInput(c, &f.name).AutoFocus() }).Error(errs.name)
				if f.local {
					ui.Text(c, "This machine is monitored directly; only its name can change.").FontSize(12).TextColor(t.TextMuted)
					return
				}
				ui.Field(c, "Connection", func() { ui.Segmented(c, &f.conn, "Tailscale / LAN", "SSH tunnel") })
				if f.conn == connDirect {
					ui.Field(c, "Address", func() { ui.TextInput(c, &f.addr).Placeholder("100.x.y.z or name.local") }).Error(errs.addr)
					ui.Field(c, "Port", func() { ui.TextInput(c, &f.port) }).Error(errs.port)
				} else {
					ui.Field(c, "Host", func() { ui.TextInput(c, &f.sshHost).Placeholder("server.example.com") }).Error(errs.sshHost)
					ui.Field(c, "Port", func() { ui.TextInput(c, &f.sshPort) }).Error(errs.sshPort)
					ui.Field(c, "User", func() { ui.TextInput(c, &f.user) })
					ui.Field(c, "Key file", func() { ui.TextInput(c, &f.key).Placeholder("~/.ssh/id_ed25519") }).Description("Optional.")
					ui.Field(c, "Agent port", func() { ui.TextInput(c, &f.agentPort) }).Error(errs.agentPort)
				}
				tok := ui.Field(c, "Token", func() { ui.TextInput(c, &f.token).Password() }).Error(errs.token)
				if f.editID != "" {
					tok.Description("Leave empty to keep the current token.")
				}
			})
			if f.saveErr != "" {
				ui.Text(c, f.saveErr).FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					f.isOpen = false
				}
				if ui.PrimaryButton(c, action).Clicked() {
					f.submitted = true
					if !f.validate().any() {
						f.submit(p)
					}
				}
			})
		})
	})
}

func (f *hostForm) submit(p *Panel) {
	var base store.Host
	adding := f.editID == ""
	if adding {
		base = store.Host{ID: newID("h")}
	} else {
		i := slices.IndexFunc(p.cfg.Hosts, func(h store.Host) bool { return h.ID == f.editID })
		if i < 0 {
			f.saveErr = "This host no longer exists."
			return
		}
		base = p.cfg.Hosts[i]
	}
	h := f.host(base)
	// The token goes first: a host saved without its token would only fail
	// to authenticate.
	if tok := strings.TrimSpace(f.token); !f.local && tok != "" {
		if err := p.eff.SetToken(h.ID, tok); err != nil {
			f.saveErr = "Could not store the token: " + err.Error()
			return
		}
	}
	err := p.edit(func(c *store.Config) error {
		if adding {
			c.Hosts = append(c.Hosts, h)
		} else {
			c.Hosts[slices.IndexFunc(c.Hosts, func(x store.Host) bool { return x.ID == h.ID })] = h
		}
		return nil
	})
	if err != nil {
		f.saveErr = p.err
		return
	}
	f.isOpen = false
	if adding {
		p.eff.HostAdded(h)
	} else {
		p.eff.HostChanged(h)
	}
}

// groupDialog names a new group, a subgroup, or renames one.
type groupDialog struct {
	isOpen    bool
	renameID  string
	parent    string
	name      string
	submitted bool
}

func (g *groupDialog) open(parent string) { *g = groupDialog{isOpen: true, parent: parent} }

func (g *groupDialog) rename(grp store.Group) {
	*g = groupDialog{isOpen: true, renameID: grp.ID, name: grp.Name}
}

func (g *groupDialog) dialog(c *ui.Context, p *Panel) {
	if !g.isOpen {
		return
	}
	title, action := "New group", "Create"
	switch {
	case g.renameID != "":
		title, action = "Rename group", "Rename"
	case g.parent != "":
		title = "New subgroup"
	}
	ui.Modal(c, &g.isOpen, func() {
		msg := ""
		if g.submitted && strings.TrimSpace(g.name) == "" {
			msg = "Enter a name."
		}
		done := false
		ui.Column(c).Width(320).Gap(16).Children(func() {
			ui.Text(c, title).FontSize(16).FontWeight(700)
			ui.Field(c, "Name", func() {
				if ui.TextInput(c, &g.name).AutoFocus().Submitted() {
					done = true
				}
			}).Error(msg)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					g.isOpen = false
				}
				if ui.PrimaryButton(c, action).Clicked() {
					done = true
				}
			})
		})
		if done {
			g.submit(p)
		}
	})
}

func (g *groupDialog) submit(p *Panel) {
	g.submitted = true
	name := strings.TrimSpace(g.name)
	if name == "" {
		return
	}
	err := p.edit(func(cfg *store.Config) error {
		if g.renameID != "" {
			i := slices.IndexFunc(cfg.Groups, func(x store.Group) bool { return x.ID == g.renameID })
			if i < 0 {
				return fmt.Errorf("this group no longer exists")
			}
			cfg.Groups[i].Name = name
			return nil
		}
		id := newID("g")
		cfg.Groups = append(cfg.Groups, store.Group{ID: id, Name: name, Parent: g.parent})
		cfg.UI.OpenGroups = append(cfg.UI.OpenGroups, id)
		if g.parent != "" && !slices.Contains(cfg.UI.OpenGroups, g.parent) {
			cfg.UI.OpenGroups = append(cfg.UI.OpenGroups, g.parent)
		}
		return nil
	})
	if err == nil {
		g.isOpen = false
	}
}
