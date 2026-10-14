package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// inspector shows the chosen process: its command line, who and when, and
// the buttons that end it. It never shows the environment: the agent does not
// read it.
func (p *Processes) inspector(c *ui.Context, in ProcessesInput, pr *model.Process) {
	t := c.Theme()
	command := pr.Command
	if command == "" {
		command = pr.Name
	}
	parent := ""
	if pr.PPID > 0 {
		parent = fmt.Sprint(pr.PPID)
		for i := range in.View.Procs {
			if q := &in.View.Procs[i]; q.PID == pr.PPID && (pr.StartTime.IsZero() || q.StartTime.IsZero() || !q.StartTime.After(pr.StartTime)) {
				parent = fmt.Sprintf("%d (%s)", pr.PPID, q.Name)
				break
			}
		}
	}
	started := format.Unknown
	if !pr.StartTime.IsZero() {
		started = pr.StartTime.Local().Format("2006-01-02 15:04:05") + " · " + format.Ago(in.Now.Sub(pr.StartTime))
	}
	threads := format.Unknown
	if pr.Threads > 0 {
		threads = fmt.Sprint(pr.Threads)
	}
	ports := format.Unknown
	if len(pr.Ports) > 0 {
		ports = portsText(pr.Ports)
	}
	target := fmt.Sprintf("%d@%d", pr.PID, startNano(pr.StartTime))
	// Without a start time the target "pid@0" would not identify the process.
	unsafe := pr.StartTime.IsZero()
	running := in.Actions.Running(in.HostID, target) || unsafe

	ui.Column(c).Padding(2 * components.Grid).Gap(2 * components.Grid).Children(func() {
		ui.Text(c, "Command").FontSize(12).FontWeight(600).TextColor(t.TextMuted)
		ui.Text(c, command).FontSize(13).Selectable()
		field := func(label, value string) {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
				ui.Text(c, value).FontSize(13).Selectable()
			})
		}
		field("PID", fmt.Sprint(pr.PID))
		if parent != "" {
			field("Parent", parent)
		}
		field("User", orDash(pr.User))
		field("Started", started)
		field("Threads", threads)
		field("Ports", ports)
		if pr.AppBundle != "" {
			field("App", pr.AppBundle)
		}
		if unsafe {
			ui.Text(c, "Start time unknown — cannot signal safely").FontSize(12).TextColor(t.TextMuted)
		}
		ui.Row(c).Gap(components.Grid).Children(func() {
			ask := func(dialog *bool) {
				p.pending = pendingKill{host: in.HostID, name: orDash(pr.Name), pid: pr.PID, nano: startNano(pr.StartTime)}
				*dialog = true
			}
			if ui.Button(c, "Terminate").Disabled(running).Clicked() {
				ask(&p.askTerm)
			}
			if ui.Button(c, "Force kill").Disabled(running).Clicked() {
				ask(&p.askKill1)
			}
		})
	})
}

// dialogs asks for the confirmations. Terminate asks once; Force kill asks
// twice. Only the last confirmation starts the action.
func (p *Processes) dialogs(c *ui.Context, in ProcessesInput) {
	who := fmt.Sprintf("%s (PID %d)", p.pending.name, p.pending.pid)
	switch ui.AlertDialog(c, &p.askTerm, "Terminate "+who+"?",
		"Sends TERM, asking the process to exit. It may save its work first.", "Cancel", "Terminate process") {
	case 1:
		p.send(in, model.SigTerm)
	}
	switch ui.AlertDialog(c, &p.askKill1, "Force kill "+who+"?",
		"KILL ends the process at once; unsaved work is lost.", "Cancel", "Continue") {
	case 1:
		p.askKill2 = true
	}
	switch ui.AlertDialog(c, &p.askKill2, "Really force kill "+who+"?",
		"This cannot be undone.", "Cancel", "Force kill process") {
	case 1:
		p.send(in, model.SigKill)
	}
}

// send starts the signal on the process that was asked about, by PID and
// start time, so that a reused PID is never signalled.
func (p *Processes) send(in ProcessesInput, sig model.Signal) {
	pk := p.pending
	in.Actions.Start(hub.Action{Kind: "signal", HostID: pk.host, State: "confirming", Verb: string(sig),
		Target: fmt.Sprintf("%d@%d", pk.pid, pk.nano)})
}
