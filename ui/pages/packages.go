package pages

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/mac/pkgs"
	"hotwatch/ui/components"
)

var pkgCols = []ui.TableColumn{
	{Title: "Name", ID: "name", MinWidth: 160},
	{Title: "Kind", ID: "kind", Width: 90, MinWidth: 60},
	{Title: "Installed", ID: "version", Width: 140, MinWidth: 80},
	{Title: "Newest", ID: "newest", Width: 140, MinWidth: 80},
	{Title: "Actions", ID: "actions", Width: 200, MinWidth: 160},
}

// pkgTabs are the managers the page switches between, in order.
var pkgTabs = []string{"Homebrew", "mise"}

// logShown is how many of a task's last lines the log pane keeps.
const logShown = 200

// PackagesInput is what the Packages page reads each frame.
type PackagesInput struct {
	HostID  string
	Now     time.Time
	Actions *Actions
	Width   float32
}

// Packages is the Packages page: Homebrew formulae and casks and mise
// tools, outdated first, with update, update all, remove and prune, and
// the running task's live log. Listing runs off the main thread; tasks run
// as "package" actions in the hub's runner.
type Packages struct {
	// Spawn runs f off the main thread; tests make it run inline.
	Spawn func(f func())

	mgr  pkgs.Manager
	post func(func())

	tab     int
	tabInit bool
	list    ui.ListState

	loading, loaded bool
	all             []pkgs.Package
	err             error
	ask             bool
	pending         pkgs.Package
	handled         hub.ActionID
}

// NewPackages returns the page over mgr; post runs a function on the main
// thread. A nil mgr (a remote host) shows a plain explanation.
func NewPackages(mgr pkgs.Manager, post func(func())) *Packages {
	p := &Packages{mgr: mgr, post: post}
	p.Spawn = func(f func()) { go f() }
	if post == nil {
		p.post = func(f func()) { f() }
	}
	return p
}

func (p *Packages) load() {
	p.loading = true
	mgr := p.mgr
	p.Spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		list, err := mgr.List(ctx)
		p.post(func() { p.all, p.err, p.loaded, p.loading = list, err, true, false })
	})
}

// mise reports whether the mise tab is shown (else Homebrew's).
func (p *Packages) mise() bool { return p.tab == 1 }

// rows are the current tab's packages, outdated first, then by name.
func (p *Packages) rows() []pkgs.Package {
	var out []pkgs.Package
	for _, x := range p.all {
		if (x.Kind == "mise") == p.mise() {
			out = append(out, x)
		}
	}
	slices.SortStableFunc(out, func(a, b pkgs.Package) int {
		if a.Outdated != b.Outdated {
			if a.Outdated {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// Render builds the page.
func (p *Packages) Render(c *ui.Context, in PackagesInput) {
	if p.mgr == nil {
		components.EmptyState(c, "Packages are managed on this Mac only")
		return
	}
	brew, mise := p.mgr.Available()
	if !p.tabInit {
		p.tabInit = true
		if !brew && mise {
			p.tab = 1
		}
	}
	if !p.loaded && !p.loading && (brew || mise) {
		p.load()
	}
	p.reloadAfterTask(in)
	t := c.Theme()
	rows := p.rows()
	outdated := 0
	for _, x := range rows {
		if x.Outdated {
			outdated++
		}
	}
	// One package task at a time: brew holds a lock while it works.
	last, started := in.Actions.Last(in.HostID, "package")
	running := started && (last.State == "running" || last.State == "confirming")
	ui.Column(c).Gap(2 * g()).Children(func() {
		ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &p.tab, pkgTabs...)
			if outdated > 0 {
				ui.Row(c).Padding(2, 10).Radius(999).Background(t.Accent.Alpha(0.12)).Children(func() {
					ui.Text(c, fmt.Sprintf("%d outdated", outdated)).FontSize(12).FontWeight(600).TextColor(t.Accent).SingleLine()
				})
			}
			ui.Spacer(c)
			if p.loading {
				ui.Text(c, "Checking…").FontSize(12).TextColor(t.TextMuted)
			}
			installed := brew
			if p.mise() {
				installed = mise
			}
			if installed {
				if ui.Button(c, "Prune").Disabled(running).Clicked() {
					p.start(in, "prune", p.prefix())
				}
				if ui.PrimaryButton(c, "Update All").Disabled(running || outdated == 0).Clicked() {
					p.start(in, "upgrade-all", p.prefix())
				}
			}
		})
		if p.mise() && !mise {
			components.EmptyState(c, "mise is not installed")
			return
		}
		if !p.mise() && !brew {
			components.EmptyState(c, "Homebrew is not installed")
			return
		}
		if p.err != nil {
			components.Banner(c, components.BannerWarning, "Could not list everything: "+p.err.Error())
		}
		_, wh := c.Size()
		h := max(wh-260, 200) // the whole page, unless a task's log shares it
		if started {
			h = max(wh-520, 200)
		}
		if p.loaded && len(rows) == 0 {
			components.EmptyState(c, "Nothing installed")
		} else {
			p.list.Key = func(r int) any { return rows[r].Kind + ":" + rows[r].Name }
			ui.Table(c, &p.list, pkgCols, len(rows), func(r, col int) { p.cell(c, in, rows[r], pkgCols[col].ID, running) }).Height(h)
		}
		p.logPane(c, in)
	})
	p.dialog(c, in)
}

// prefix is the target of a whole-manager task: "" for Homebrew, "mise:".
func (p *Packages) prefix() string {
	if p.mise() {
		return "mise:"
	}
	return ""
}

func target(x pkgs.Package) string { return x.Kind + ":" + x.Name }

// start hands a confirmed task to the runner. Every package task shares
// one conflict key, so two brew runs never overlap (brew holds a lock).
func (p *Packages) start(in PackagesInput, verb, tgt string) {
	in.Actions.Start(hub.Action{Kind: "package", HostID: in.HostID, State: "confirming", Verb: verb, Target: tgt})
}

func (p *Packages) cell(c *ui.Context, in PackagesInput, x pkgs.Package, id string, running bool) {
	t := c.Theme()
	text := func(s string) { ui.Text(c, s).FontSize(13).SingleLine() }
	switch id {
	case "name":
		text(x.Name)
	case "kind":
		ui.Text(c, x.Kind).FontSize(12).TextColor(t.TextMuted).SingleLine()
	case "version":
		text(orDash(x.Version))
	case "newest":
		if x.Outdated {
			ui.Text(c, x.Newest).FontSize(13).FontWeight(600).TextColor(t.Accent).SingleLine()
		} else {
			ui.Text(c, orDash(x.Newest)).FontSize(13).TextColor(t.TextMuted).SingleLine()
		}
	case "actions":
		ui.Row(c).Gap(g()).Children(func() {
			if x.Outdated && ui.Button(c, "Update").Disabled(running).Clicked() {
				p.start(in, "upgrade", target(x))
			}
			if ui.Button(c, "Remove").Disabled(running).Clicked() {
				p.pending, p.ask = x, true
			}
		})
	}
}

// exitRE finds the exit status in a failed task's message.
var exitRE = regexp.MustCompile(`exit status \d+`)

// logPane shows the newest package task: what it runs, its state, and its
// last lines; a failure keeps its exit status and output on screen.
func (p *Packages) logPane(c *ui.Context, in PackagesInput) {
	a, ok := in.Actions.Last(in.HostID, "package")
	if !ok {
		return
	}
	t := c.Theme()
	components.Surface(c, ui.Column(c).Gap(g())).Children(func() {
		ui.Row(c).Gap(g()).AlignItems(ui.Center).Children(func() {
			state := string(a.State)
			if state == "succeeded" {
				state = "Done"
			}
			components.Badge(c, state)
			ui.Text(c, strings.TrimSpace(a.Verb+" "+a.Target)).Font("monospace").FontSize(13).SingleLine()
			if a.State == "failed" && a.Err != nil {
				if m := exitRE.FindString(a.Err.Message); m != "" {
					ui.Text(c, m).FontSize(12).FontWeight(600).TextColor(t.Danger).SingleLine()
				} else {
					ui.Text(c, a.Err.Message).FontSize(12).TextColor(t.Danger).SingleLine()
				}
			}
		})
		lines := a.Log
		if len(lines) > logShown {
			lines = lines[len(lines)-logShown:]
		}
		if len(lines) == 0 {
			note(c, "No output yet")
			return
		}
		ui.Scroll(c).Height(min(float32(len(lines))*18+8, 220)).Children(func() {
			ui.Column(c).Padding(4, 0).Children(func() {
				for _, l := range lines {
					ui.Text(c, l).Font("monospace").FontSize(12).Selectable()
				}
			})
		})
	})
}

// dialog confirms a removal; only its confirm button starts it.
func (p *Packages) dialog(c *ui.Context, in PackagesInput) {
	x := p.pending
	if ui.AlertDialog(c, &p.ask, "Remove "+x.Name+"?",
		"The "+x.Kind+" is uninstalled. Anything that depends on it may stop working.",
		"Cancel", "Remove") == 1 {
		p.start(in, "remove", target(x))
	}
}

// reloadAfterTask lists again once a task finished, so versions and the
// outdated badge follow what it did.
func (p *Packages) reloadAfterTask(in PackagesInput) {
	a, ok := in.Actions.Last(in.HostID, "package")
	if !ok || a.ID == p.handled || a.State == "running" || a.State == "confirming" {
		return
	}
	p.handled = a.ID
	if !p.loading {
		p.load()
	}
}
