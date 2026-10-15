package ui

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/store"
	"hotwatch/ui/components"
)

// Hooks the application (cmd/hotwatch) drives the App through. Like the rest
// of App, call them on the main thread.

// SelectHost shows host id, as a click in the host panel does. Unknown ids are
// ignored.
func (a *App) SelectHost(id string) {
	if slices.ContainsFunc(a.hosts, func(h store.Host) bool { return h.ID == id }) {
		a.selectHost(id)
	}
}

// SetHosts replaces the host list after hosts were added, edited or removed.
// Views of removed hosts are dropped; when the selected host is gone, "local"
// (else the first host) is selected.
func (a *App) SetHosts(hosts []store.Host) {
	a.hosts = slices.Clone(hosts)
	has := func(id string) bool {
		return slices.ContainsFunc(a.hosts, func(h store.Host) bool { return h.ID == id })
	}
	for id := range a.views {
		if !has(id) {
			delete(a.views, id)
		}
	}
	a.gen++
	if has(a.selected) || len(a.hosts) == 0 {
		return
	}
	next := a.hosts[0].ID
	if has("local") {
		next = "local"
	}
	a.selectHost(next)
}

// CloseLogs ends the Docker page's log stream, if one is open.
func (a *App) CloseLogs() { a.docker.CloseLogs() }

// SetNotice shows text in a warning banner above every page until the user
// dismisses it.
func (a *App) SetNotice(text string) { a.notice = text }

// SetFrameHook sets a function called at the end of every frame with the time
// the frame started.
func (a *App) SetFrameHook(f func(start time.Time)) { a.onFrame = f }

type passPrompt struct {
	keyPath string
	reply   func(pass string, ok bool)
}

// AskPassphrase queues a prompt for the passphrase of an encrypted SSH key.
// reply is called at most once, on the main thread: with the passphrase and
// true, or with "" and false when the user cancels. The returned cancel
// withdraws the prompt without calling reply (the dial gave up); call it on
// the main thread. The passphrase is held only until reply is called.
func (a *App) AskPassphrase(keyPath string, reply func(pass string, ok bool)) (cancel func()) {
	p := &passPrompt{keyPath: keyPath, reply: reply}
	a.prompts = append(a.prompts, p)
	return func() {
		i := slices.Index(a.prompts, p)
		if i < 0 {
			return
		}
		if i == 0 {
			a.pass, a.passOpen = "", false
		}
		a.prompts = slices.Delete(a.prompts, i, i+1)
	}
}

// SetRetry sets what the Retry button of a host that is offline, not
// authorized or incompatible does. Without one there is no button.
func (a *App) SetRetry(f func(hostID string)) { a.onRetry = f }

// retryButton offers Retry for a host whose connection stopped trying, or
// is backing off.
func (a *App) retryButton(c *ui.Context, st hub.ConnState) {
	if a.onRetry == nil {
		return
	}
	switch st {
	case hub.Offline, hub.Unauthorized, hub.Incompatible:
		if ui.Button(c, "Retry").Clicked() {
			a.onRetry(a.selected)
		}
	}
}

func (a *App) noticeBanner(c *ui.Context) {
	if a.notice == "" {
		return
	}
	ui.Row(c).Gap(components.Grid).AlignItems(ui.Center).Children(func() {
		components.Banner(c, components.BannerWarning, a.notice).Grow(1)
		if ui.Button(c, "Dismiss").Clicked() {
			a.notice = ""
		}
	})
}

// passphraseDialog shows the first queued passphrase prompt.
func (a *App) passphraseDialog(c *ui.Context) {
	if len(a.prompts) == 0 {
		return
	}
	p := a.prompts[0]
	answered := false
	answer := func(pass string, ok bool) {
		answered = true
		a.prompts = a.prompts[1:]
		a.pass, a.passOpen = "", false
		p.reply(pass, ok)
		c.Invalidate()
	}
	a.passOpen = true
	t := c.Theme()
	ui.Modal(c, &a.passOpen, func() {
		ui.Column(c).Width(420).Gap(12).Children(func() {
			ui.Text(c, "Unlock SSH key").FontSize(16).FontWeight(700)
			ui.Text(c, "Enter the passphrase for").FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, p.keyPath).Font("monospace").SingleLine()
			submitted := ui.TextInput(c, &a.pass).Password().AutoFocus().Submitted()
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					answer("", false)
					return
				}
				if ui.PrimaryButton(c, "Unlock").Clicked() || submitted {
					answer(a.pass, true)
				}
			})
		})
	})
	if !a.passOpen && !answered {
		answer("", false) // dismissed with Escape or a click outside
	}
}

// Selected is the host the pages show.
func (a *App) Selected() string { return a.selected }

// Hosts are the configured hosts, in order.
func (a *App) Hosts() []store.Host { return slices.Clone(a.hosts) }

// HostView is the latest view of host id.
func (a *App) HostView(id string) hub.HostView { return a.views[id] }

// ShowPage opens page on the selected host, as a sidebar click does; a
// page the host does not have is ignored.
func (a *App) ShowPage(page string) {
	if slices.ContainsFunc(visiblePages(a.views[a.selected].Info.Caps), func(p Page) bool { return p.Key == page }) {
		a.setPage(page)
	}
}

// TogglePause holds updates back, or resumes them.
func (a *App) TogglePause() { a.togglePause() }
