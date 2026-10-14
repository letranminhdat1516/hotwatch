package hostpanel

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/ui/components"
)

const mismatchText = "Host key changed — possible attack. Remove the old key from known_hosts by hand."

// hostKeyState is the host-key dialog. It opens by itself the first time a
// fingerprint is shown for a host, then only when the user asks.
type hostKeyState struct {
	isOpen bool
	seen   map[string]bool
	err    string
}

func hostKeyFor(selected string, view func(string) hub.HostView) (hub.HostView, bool, bool) {
	v := view(selected)
	if v.HostKey == nil {
		return v, false, false
	}
	return v, v.Conn.State == hub.HostKeyUnknown && !v.HostKey.Mismatch,
		v.Conn.State == hub.HostKeyMismatch || v.HostKey.Mismatch
}

// hostKeyNote reports whether the selected host has something to say about
// its host key, and draws it when draw is set.
func (p *Panel) hostKeyNote(c *ui.Context, selected string, view func(string) hub.HostView, draw bool) bool {
	_, unknown, mismatch := hostKeyFor(selected, view)
	if !draw {
		return unknown || mismatch
	}
	t := c.Theme()
	switch {
	case mismatch:
		ui.Text(c, mismatchText).FontSize(12).TextColor(t.Danger)
	case unknown:
		ui.Text(c, "The SSH host key is not verified yet.").FontSize(12).TextColor(t.Warning)
		if ui.Button(c, "Verify host key…").Clicked() {
			p.hk.isOpen, p.hk.err = true, ""
		}
	}
	return unknown || mismatch
}

func (p *Panel) hostKeyDialog(c *ui.Context, selected string, view func(string) hub.HostView) {
	v, unknown, _ := hostKeyFor(selected, view)
	if !unknown {
		p.hk.isOpen = false
		return
	}
	hk := v.HostKey
	key := selected + "|" + hk.Fingerprint
	if p.hk.seen == nil {
		p.hk.seen = map[string]bool{}
	}
	if !p.hk.seen[key] {
		p.hk.seen[key], p.hk.isOpen, p.hk.err = true, true, ""
	}
	t := c.Theme()
	ui.Modal(c, &p.hk.isOpen, func() {
		ui.Column(c).Width(440).Gap(12).Children(func() {
			ui.Text(c, "Verify host key").FontSize(16).FontWeight(700)
			ui.Text(c, fmt.Sprintf("The SSH host key for %s is not known yet.", hk.Host))
			ui.Text(c, "Fingerprint").FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, hk.Fingerprint).Font("monospace").Selectable().Padding(8).Radius(components.Grid).Background(t.Background)
			ui.Text(c, "Compare it with the output of `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the server, and trust it only if they match.").FontSize(12).TextColor(t.TextMuted)
			if p.hk.err != "" {
				ui.Text(c, p.hk.err).FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					p.hk.isOpen = false
				}
				if ui.PrimaryButton(c, "Trust").Clicked() {
					// The exact error the hub published, never a re-fetched key.
					if err := p.eff.TrustHostKey(selected, hk); err != nil {
						p.hk.err = "Could not trust the key: " + err.Error()
					} else {
						p.hk.isOpen = false
					}
				}
			})
		})
	})
}
