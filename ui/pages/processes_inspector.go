package pages

import (
	"fmt"
	"path"
	"strings"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
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

	g := float32(components.Grid)
	ui.Column(c).Padding(2*g, 2*g).Gap(2 * g).Children(func() {
		// Header: the icon, the name, PID and user, and a close button.
		ui.Row(c).Gap(g + 4).AlignItems(ui.Center).Children(func() {
			appIconSized(c, pr.Name, pr.AppBundle, 44)
			ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, orDash(pr.Name)).FontSize(18).FontWeight(700).SingleLine()
				ui.Text(c, fmt.Sprintf("PID %d · %s", pr.PID, orDash(pr.User))).FontSize(13).TextColor(t.TextMuted).SingleLine()
			})
			closeBtn := ui.Row(c).Key("proc-close").Size(24, 24).Center().Radius(12).Cursor(ui.CursorPointer).Label("Close")
			if closeBtn.Hovered() {
				closeBtn.Background(t.SurfaceHover)
			}
			if closeBtn.Clicked() {
				p.hasSel, p.row = false, -1
			}
			closeBtn.Children(func() { components.Glyph(c, icons.Close, 14, t.TextMuted) })
		})
		// Live figures as tiles, each in its metric's colour.
		tile := func(key, label, value string) {
			tint := metricTint(c, key)
			ui.Column(c).Padding(10, 12).Radius(12).Gap(2).Background(t.SurfaceHover).Children(func() {
				ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(tint).SingleLine()
				ui.Text(c, value).FontSize(17).FontWeight(700).SingleLine()
			})
		}
		ui.Grid(c).Columns(2).Gap(g).Children(func() {
			tile("cpu", "CPU", pctShort(pr.CPUPct))
			mem := format.Unknown
			if m := pr.Mem(); m > 0 {
				mem = format.Bytes(m)
			}
			tile("memory", "Memory", mem)
			if pr.DiskReadBps != nil || pr.DiskWriteBps != nil {
				tile("disk", "Disk", format.Rate(pr.DiskReadBps)+" ↓ "+format.Rate(pr.DiskWriteBps)+" ↑")
			}
			if pr.NetRxBps != nil || pr.NetTxBps != nil {
				tile("network", "Network", format.Rate(pr.NetRxBps)+" ↓ "+format.Rate(pr.NetTxBps)+" ↑")
			}
			if pr.PowerMW != nil {
				tile("energy", "Energy", watts(*pr.PowerMW))
			}
			tile("", "Threads", threads)
		})
		ui.Row(c).Gap(g).Children(func() {
			ask := func(dialog *bool) {
				p.pending = pendingKill{host: in.HostID, name: orDash(pr.Name), pid: pr.PID, nano: startNano(pr.StartTime)}
				*dialog = true
			}
			if ui.Button(c, "Terminate").Grow(1).Disabled(running).Clicked() {
				ask(&p.askTerm)
			}
			kill := ui.Button(c, "Force kill").Grow(1).Disabled(running)
			if !running {
				kill.TextColor(t.Danger)
			}
			if kill.Clicked() {
				ask(&p.askKill1)
			}
		})
		if unsafe {
			ui.Text(c, "Start time unknown — cannot signal safely").FontSize(12).TextColor(t.TextMuted)
		}
		ui.Text(c, "DETAILS").FontSize(12).FontWeight(650).TextColor(t.TextMuted).Margin(g/2, 0, 0, 0)
		ui.Column(c).Gap(g).Children(func() {
			kvRow(c, "PID", fmt.Sprint(pr.PID))
			if parent != "" {
				kvRow(c, "Parent", parent)
			}
			kvRow(c, "User", orDash(pr.User))
			kvRow(c, "Started", started)
			kvRow(c, "Ports", ports)
			if pr.AppBundle != "" {
				kvRow(c, "App", strings.TrimSuffix(path.Base(outerBundle(pr.AppBundle)), ".app"))
			}
		})
		ui.Text(c, "COMMAND").FontSize(12).FontWeight(650).TextColor(t.TextMuted).Margin(g/2, 0, 0, 0)
		ui.Column(c).Padding(10, 12).Radius(10).Background(t.SurfaceHover).Children(func() {
			ui.Text(c, command).FontSize(12).Font("Menlo").Selectable()
		})
		if pr.AppBundle != "" {
			ui.Text(c, "PATH").FontSize(12).FontWeight(650).TextColor(t.TextMuted).Margin(g/2, 0, 0, 0)
			ui.Text(c, pr.AppBundle).FontSize(12).Font("Menlo").TextColor(t.TextMuted).Selectable()
		}
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
