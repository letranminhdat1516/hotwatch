package pages

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

const noProject = "No project"

// dockerCols are the container table's columns.
var dockerCols = []ui.TableColumn{
	{Title: "Name", ID: "name", MinWidth: 120},
	{Title: "Image", ID: "image", Width: 150, MinWidth: 80},
	{Title: "State", ID: "state", Width: 118, MinWidth: 100},
	{Title: "Health", ID: "health", Width: 80, MinWidth: 60},
	{Title: "CPU", ID: "cpu", Width: 70, MinWidth: 50, Align: ui.End},
	{Title: "Memory", ID: "mem", Width: 140, MinWidth: 90, Align: ui.End},
	{Title: "Ports", ID: "ports", Width: 130, MinWidth: 60},
	{Title: "Uptime", ID: "uptime", Width: 80, MinWidth: 60, Align: ui.End},
	{Title: "Actions", ID: "actions", Width: 300, MinWidth: 240},
}

// DockerInput is what the Docker page reads each frame.
type DockerInput struct {
	HostID  string
	View    hub.HostView
	Gen     uint64 // changes whenever View does; grouping reruns only then
	Now     time.Time
	Actions *Actions
}

// dockerRow is a row of the grouped table: a project header or a container.
type dockerRow struct {
	project string // header rows
	ctr     int    // index into the container list; -1 for a header
}

// pendingCtr is the container a confirmation dialog asks about, fixed when
// the dialog opens so a refresh cannot change what Confirm acts on.
type pendingCtr struct {
	host, id, name, verb string
}

// Docker is the Docker page and its state. Main thread only, except for the
// log reader, which reaches the page only through the poster.
type Docker struct {
	opener LogsOpener
	post   func(func())
	logs   logPane

	host string
	ctrs []model.Container
	rows []dockerRow
	key  struct {
		host string
		gen  uint64
		n    int
		ok   bool
	}
	list ui.ListState

	ask     bool
	pending pendingCtr

	closeRequested, reopen bool
	openRequest            string
}

// NewDocker returns the page with the log pane closed, 100 lines, following.
func NewDocker() *Docker {
	d := &Docker{}
	d.logs.follow = true
	return d
}

func (d *Docker) sync(in DockerInput) {
	if d.host != in.HostID {
		d.CloseLogs()
		d.host = in.HostID
		d.ask = false
		d.key.ok = false
	}
}

// ensure groups the containers by compose project: projects by name with
// "No project" last, containers by name inside each.
func (d *Docker) ensure(in DockerInput) {
	k := in.View.Ctrs
	if d.key.ok && d.key.host == in.HostID && d.key.gen == in.Gen && d.key.n == len(k) {
		return
	}
	d.key.host, d.key.gen, d.key.n, d.key.ok = in.HostID, in.Gen, len(k), true
	d.ctrs = k
	idx := make([]int, len(k))
	for i := range idx {
		idx[i] = i
	}
	proj := func(i int) string { return k[i].Project }
	slices.SortStableFunc(idx, func(a, b int) int {
		pa, pb := proj(a), proj(b)
		if (pa == "") != (pb == "") {
			if pa == "" {
				return 1
			}
			return -1
		}
		if c := strings.Compare(pa, pb); c != 0 {
			return c
		}
		return strings.Compare(k[a].Name, k[b].Name)
	})
	d.rows = d.rows[:0]
	for n, i := range idx {
		if n == 0 || proj(i) != proj(idx[n-1]) {
			name := proj(i)
			if name == "" {
				name = noProject
			}
			d.rows = append(d.rows, dockerRow{project: name, ctr: -1})
		}
		d.rows = append(d.rows, dockerRow{ctr: i})
	}
}

func isNotFound(err error) bool {
	var v model.APIError
	if errors.As(err, &v) {
		return v.Code == "not_found"
	}
	var p *model.APIError
	return errors.As(err, &p) && p != nil && p.Code == "not_found"
}

// Render builds the page.
func (d *Docker) Render(c *ui.Context, in DockerInput) {
	d.sync(in)
	v := in.View
	if isNotFound(v.CtrsErr) {
		components.EmptyState(c, "Docker is not monitored on this Mac")
		return
	}
	if !v.Info.Caps.Docker {
		components.EmptyState(c, "Docker not available on this host")
		return
	}
	d.ensure(in)
	d.checkListed(in)
	ui.Column(c).Gap(2 * g()).Children(func() {
		if err := v.CtrsErr; err != nil {
			components.Banner(c, components.BannerWarning, "Containers may be stale: "+err.Error())
		}
		if len(v.Ctrs) == 0 {
			components.EmptyState(c, "No containers")
			return
		}
		d.content(c, in)
	})
	d.dialog(c, in)

	// What the frame asked for is done after it, never while building it.
	switch {
	case d.closeRequested:
		d.closeRequested, d.reopen = false, false
		d.CloseLogs()
		c.Invalidate()
	case d.openRequest != "":
		id := d.openRequest
		d.openRequest, d.reopen = "", false
		d.OpenLogs(id)
		c.Invalidate()
	case d.reopen:
		d.reopen = false
		d.startStream()
		c.Invalidate()
	}
}

func (d *Docker) content(c *ui.Context, in DockerInput) {
	_, wh := c.Size()
	h := max(wh-300, 240)
	d.logs.mu.Lock()
	open := d.logs.open
	d.logs.mu.Unlock()
	if !open {
		d.table(c, in).Height(h)
		return
	}
	if d.logs.split == 0 {
		d.logs.split = h * 0.5
	}
	ui.SplitVertical(c, &d.logs.split, func() { d.table(c, in).Fill() }, func() { d.logsView(c) }).Height(h)
}

func (d *Docker) table(c *ui.Context, in DockerInput) ui.Element {
	t := c.Theme()
	st := &d.list
	st.Key = func(r int) any {
		if row := d.rows[r]; row.ctr >= 0 {
			return d.ctrs[row.ctr].ID
		} else {
			return "project:" + row.project
		}
	}
	return ui.Table(c, st, dockerCols, len(d.rows), func(r, col int) {
		row := d.rows[r]
		if row.ctr < 0 {
			if col == 0 {
				ui.Text(c, row.project).FontSize(13).FontWeight(600).TextColor(t.TextMuted).SingleLine()
			} else {
				ui.Text(c, "").FontSize(13)
			}
			return
		}
		d.cell(c, in, &d.ctrs[row.ctr], dockerCols[col].ID)
	})
}

func (d *Docker) cell(c *ui.Context, in DockerInput, ct *model.Container, id string) {
	cellText := func(s string) { ui.Text(c, s).FontSize(13).SingleLine() }
	switch id {
	case "name":
		cellText(orDash(ct.Name))
	case "image":
		cellText(orDash(ct.Image))
	case "state":
		components.Badge(c, ct.State)
	case "health":
		cellText(orDash(ct.Health))
	case "cpu":
		ui.Text(c, format.Percent(ct.CPUPct)).FontSize(13).SingleLine().TextAlign(ui.End)
	case "mem":
		ui.Text(c, memText(ct)).FontSize(13).SingleLine().TextAlign(ui.End)
	case "ports":
		cellText(orDash(strings.Join(ct.Ports, ", ")))
	case "uptime":
		s := format.Unknown
		if ct.State == "running" && !ct.StartedAt.IsZero() && !in.Now.Before(ct.StartedAt) {
			s = format.Duration(in.Now.Sub(ct.StartedAt))
		}
		ui.Text(c, s).FontSize(13).SingleLine().TextAlign(ui.End)
	case "actions":
		d.actions(c, in, ct)
	}
}

func memText(ct *model.Container) string {
	if ct.MemUsed == nil && ct.MemLimit == nil {
		return format.Unknown
	}
	used, limit := format.Unknown, format.Unknown
	if ct.MemUsed != nil {
		used = format.Bytes(*ct.MemUsed)
	}
	if ct.MemLimit != nil && *ct.MemLimit > 0 {
		limit = format.Bytes(*ct.MemLimit)
	}
	return used + " / " + limit
}

// actions draws a container's buttons, which depend on its state, and the
// latest result of an action on it.
func (d *Docker) actions(c *ui.Context, in DockerInput, ct *model.Container) {
	t := c.Theme()
	running := in.Actions.Running(in.HostID, ct.ID)
	ask := func(verb string) {
		d.pending = pendingCtr{host: in.HostID, id: ct.ID, name: orDash(ct.Name), verb: verb}
		d.ask = true
	}
	ui.Column(c).Gap(2).Children(func() {
		ui.Row(c).Gap(g()).Children(func() {
			switch ct.State {
			case "running", "restarting", "paused":
				if ui.Button(c, "Stop").Disabled(running).Clicked() {
					ask(string(model.ActionStop))
				}
				if ct.State != "paused" && ui.Button(c, "Restart").Disabled(running).Clicked() {
					ask(string(model.ActionRestart))
				}
			default:
				if ui.Button(c, "Start").Disabled(running).Clicked() {
					ask(string(model.ActionStart))
				}
			}
			if ui.Button(c, "Logs").Clicked() {
				d.openRequest = ct.ID
			}
		})
		if a, ok := in.Actions.LastOn(in.HostID, "container", ct.ID); ok {
			text, col := actionResult(a), t.TextMuted
			if a.State == "failed" {
				col = t.Danger
			}
			ui.Text(c, text).FontSize(12).TextColor(col).SingleLine()
		}
	})
}

func actionResult(a hub.Action) string {
	verb := a.Verb
	if verb != "" {
		verb = strings.ToUpper(verb[:1]) + verb[1:]
	}
	s := fmt.Sprintf("%s: %s", verb, a.State)
	if a.State == "failed" && a.Err != nil {
		s += ": " + a.Err.Message
	}
	return s
}

// dialog asks for the confirmation. Only Confirm starts the action, on the
// container that was asked about.
func (d *Docker) dialog(c *ui.Context, in DockerInput) {
	p := d.pending
	verb := p.verb
	title := p.name + "?"
	body := ""
	label := "Confirm"
	if verb != "" {
		cap := strings.ToUpper(verb[:1]) + verb[1:]
		title, label = cap+" "+p.name+"?", cap+" container"
		switch verb {
		case string(model.ActionStop):
			body = "The container stops; anything not saved to a volume may be lost."
		case string(model.ActionRestart):
			body = "The container stops and starts again."
		default:
			body = "The container starts."
		}
	}
	if ui.AlertDialog(c, &d.ask, title, body, "Cancel", label) == 1 {
		in.Actions.Start(hub.Action{Kind: "container", HostID: p.host, State: "confirming", Verb: p.verb, Target: p.id})
	}
}

// checkListed stops the stream of a container that left the list; the pane
// stays, saying so.
func (d *Docker) checkListed(in DockerInput) {
	p := &d.logs
	p.mu.Lock()
	open, gone, id := p.open, p.gone, p.id
	p.mu.Unlock()
	if !open || gone || slices.ContainsFunc(in.View.Ctrs, func(c model.Container) bool { return c.ID == id }) {
		return
	}
	d.stopStream()
	p.mu.Lock()
	p.gone = true
	p.mu.Unlock()
}
