package pages_test

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	hwui "hotwatch/ui"
	"hotwatch/ui/format"
	"hotwatch/ui/pages"
)

var started = time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)

func proc(pid int32, name string, cpu float64) model.Process {
	return model.Process{PID: pid, PPID: 1, Name: name, User: "alice", Command: "/usr/bin/" + name + " --flag",
		StartTime: started.Add(time.Duration(pid) * time.Second), CPUPct: cpu, RSS: uint64(pid) << 20, Threads: 4}
}

// fixtureProcs is n processes: pid 4242 "sleep" (CPU 0), pid 77 "chrome" (CPU 90)
// and n-2 "worker-N" processes below 50 % CPU.
func fixtureProcs(n int) []model.Process {
	ps := make([]model.Process, 0, n)
	ps = append(ps, proc(4242, "sleep", 0), proc(77, "chrome", 90))
	for i := 0; i < n-2; i++ {
		ps = append(ps, proc(int32(10000+i), fmt.Sprintf("worker-%d", i), float64(i%50)))
	}
	return ps
}

// pos is the index of the first text s, -1 when absent.
func pos(tt *ui.Tester, s string) int { return slices.Index(tt.Texts(), s) }

func before(t *testing.T, tt *ui.Tester, a, b string) {
	t.Helper()
	ia, ib := pos(tt, a), pos(tt, b)
	if ia < 0 || ib < 0 || ia >= ib {
		t.Fatalf("want %q before %q, got %d and %d in %q", a, b, ia, ib, tt.Texts())
	}
}

// env drives the Processes page on its own, without the shell, so that
// column headers are not confused with the top bar's tabs.
type env struct {
	tt   *ui.Tester
	page *pages.Processes
	run  *hub.FakeRunner
	acts *pages.Actions
	view hub.HostView
	gen  uint64
	cols [][]string
}

func newEnv(ps []model.Process) *env {
	e := &env{page: pages.NewProcesses(), run: &hub.FakeRunner{}, view: hub.HostView{Procs: ps}}
	e.acts = pages.NewActions(e.run)
	e.page.OnColumns = func(c []string) { e.cols = append(e.cols, slices.Clone(c)) }
	e.tt = ui.NewTester(func(c *ui.Context) {
		c.SetTheme(hwui.Theme(false))
		e.page.Render(c, pages.ProcessesInput{HostID: "h1", View: e.view, Gen: e.gen, Now: started.Add(3 * time.Hour),
			Width: 1360, Actions: e.acts})
	}, 1400, 900)
	return e
}

func (e *env) click(t *testing.T, s string) {
	t.Helper()
	if err := e.tt.Click(s); err != nil {
		t.Fatal(err)
	}
}

func (e *env) search(t *testing.T, q string) {
	t.Helper()
	e.click(t, "Search")
	e.tt.Type(q)
}

func (e *env) names() map[string]bool {
	m := map[string]bool{}
	for _, x := range e.tt.Texts() {
		m[x] = true
	}
	return m
}

func TestProcessesSortSearchAndKill(t *testing.T) {
	procs := fixtureProcs(3000)
	hosts := []store.Host{{ID: "h1", Name: "Mac", Kind: "local"}}
	runner := &hub.FakeRunner{}
	app := hwui.NewApp(func(string) hub.HostView { return hub.HostView{Procs: procs} }, hosts, runner, func(string, string) {})
	tt := ui.NewTester(app.View, 1400, 900)
	start := time.Now()
	if err := tt.Click("Processes"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("render took %v", d)
	}
	// Sorted by CPU, largest first, by default.
	before(t, tt, "chrome", "worker-49")
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	tt.Type("sleep")
	if err := tt.Click("sleep"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Terminate"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Terminate process"); err != nil {
		t.Fatal(err)
	}
	if len(runner.Started) != 1 {
		t.Fatalf("started %+v", runner.Started)
	}
	want := hub.Action{ID: 1, Kind: "signal", HostID: "h1", Verb: "TERM", State: "confirming",
		Target: fmt.Sprintf("4242@%d", procs[0].StartTime.UnixNano())}
	if got := runner.Started[0]; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("started %+v, want %+v", got, want)
	}
}

func TestProcessesRenderFastWith3000Rows(t *testing.T) {
	e := newEnv(fixtureProcs(3000))
	start := time.Now()
	for i := 0; i < 20; i++ {
		e.tt.Frame()
	}
	if per := time.Since(start) / 20; per > 100*time.Millisecond {
		t.Fatalf("frame took %v", per)
	}
}

func TestProcessesSortDescendingFirstThenAscending(t *testing.T) {
	e := newEnv([]model.Process{proc(5, "bravo", 1), proc(6, "alpha", 2), proc(7, "charlie", 3)})
	before(t, e.tt, "charlie", "alpha") // default: CPU descending
	e.click(t, "Name")
	before(t, e.tt, "charlie", "bravo") // descending by name
	before(t, e.tt, "bravo", "alpha")
	e.click(t, "Name")
	before(t, e.tt, "alpha", "bravo")
	before(t, e.tt, "bravo", "charlie")
}

func TestProcessesUnknownValuesSortLast(t *testing.T) {
	ps := []model.Process{proc(1, "big", 1), proc(2, "small", 1), proc(3, "ghost", 1)}
	ps[0].RSS, ps[1].RSS, ps[2].RSS = 2<<30, 1<<20, 0
	e := newEnv(ps)
	e.click(t, "Memory")
	before(t, e.tt, "big", "small")
	before(t, e.tt, "small", "ghost")
	if !e.names()["—"] {
		t.Fatalf("unknown memory not shown as a dash: %q", e.tt.Texts())
	}
	e.click(t, "Memory") // ascending
	before(t, e.tt, "small", "big")
	before(t, e.tt, "big", "ghost")
}

func TestProcessesSearchTokens(t *testing.T) {
	ps := []model.Process{proc(123, "python3", 1), proc(124, "postgres", 1), proc(125, "Chrome", 1), proc(126, "sleep", 1)}
	ps[0].Ports = []model.Port{{Number: 8765, Proto: "tcp"}}
	ps[0].User = "root"
	ps[1].Ports = []model.Port{{Number: 5432, Proto: "tcp"}}
	ps[1].User = "postgres"
	ps[3].Command = "/bin/sleep 600 --token-xyz"
	e := newEnv(ps)
	check := func(q string, want ...string) {
		t.Helper()
		e.tt.Key(ui.Cmd, ui.KeyA)
		e.tt.Type(q)
		got := e.names()
		for _, n := range []string{"python3", "postgres", "Chrome", "sleep"} {
			if got[n] != slices.Contains(want, n) {
				t.Errorf("query %q: %q shown=%v, want %v", q, n, got[n], slices.Contains(want, n))
			}
		}
	}
	e.search(t, "chrome")
	check("chrome", "Chrome")     // case-insensitive name
	check("token-xyz", "sleep")   // command
	check("POSTGRES", "postgres") // user and name
	check("port:8765", "python3")
	check("port:876") // exact, not a prefix
	check("user:root", "python3")
	check("user:ro")
	check("pid:124", "postgres")
	check("pid:12")
	check("user:postgres port:5432", "postgres")
	check("user:postgres port:8765")
	check("user:root python", "python3")
	check("user:root chrome")
}

func TestProcessesTreeOrphansAtRoot(t *testing.T) {
	ps := []model.Process{
		{PID: 1, PPID: 0, Name: "init", StartTime: started},
		{PID: 10, PPID: 1, Name: "shell", StartTime: started.Add(time.Second)},
		{PID: 11, PPID: 10, Name: "editor", StartTime: started.Add(2 * time.Second)},
		{PID: 20, PPID: 999, Name: "orphan", StartTime: started.Add(time.Second)},
	}
	tree := pages.BuildTree(ps, nil, func(a, b int) bool { return ps[a].PID < ps[b].PID })
	if got := tree.Roots; !slices.Equal(got, []int{0, 3}) {
		t.Fatalf("roots %v, want init and orphan", got)
	}
	if got := tree.Children[1]; !slices.Equal(got, []int{2}) {
		t.Fatalf("children of shell %v", got)
	}
	e := newEnv(ps)
	e.click(t, "Tree view")
	for _, n := range []string{"init", "shell", "editor", "orphan"} {
		if !e.names()[n] {
			t.Errorf("tree lacks %q: %q", n, e.tt.Texts())
		}
	}
	before(t, e.tt, "init", "shell")
	before(t, e.tt, "shell", "editor")
}

func TestProcessesTreeBreaksCyclesAndPIDReuse(t *testing.T) {
	ps := []model.Process{
		{PID: 1, PPID: 2, Name: "a", StartTime: started},
		{PID: 2, PPID: 1, Name: "b", StartTime: started},
		{PID: 3, PPID: 4, Name: "child", StartTime: started}, // parent's pid reused later
		{PID: 4, PPID: 0, Name: "reused", StartTime: started.Add(time.Hour)},
	}
	tree := pages.BuildTree(ps, nil, func(a, b int) bool { return a < b })
	seen := map[int]bool{}
	var walk func(i int)
	walk = func(i int) {
		if seen[i] {
			t.Fatalf("node %d reached twice", i)
		}
		seen[i] = true
		for _, c := range tree.Children[i] {
			walk(c)
		}
	}
	for _, r := range tree.Roots {
		walk(r)
	}
	if len(seen) != 4 {
		t.Fatalf("only %v reachable from roots %v", seen, tree.Roots)
	}
	if !slices.Contains(tree.Roots, 2) { // "child" started before its supposed parent
		t.Fatalf("child of a process that started later must be a root: %v", tree.Roots)
	}
}

func TestProcessesTreeFilterKeepsAncestors(t *testing.T) {
	ps := []model.Process{
		{PID: 1, PPID: 0, Name: "init", StartTime: started},
		{PID: 10, PPID: 1, Name: "shell", StartTime: started},
		{PID: 11, PPID: 10, Name: "editor", StartTime: started},
		{PID: 12, PPID: 10, Name: "other", StartTime: started},
	}
	match := []bool{false, false, true, false}
	tree := pages.BuildTree(ps, match, func(a, b int) bool { return a < b })
	if !slices.Equal(tree.Roots, []int{0}) || !slices.Equal(tree.Children[0], []int{1}) ||
		!slices.Equal(tree.Children[1], []int{2}) {
		t.Fatalf("tree %+v", tree)
	}
}

func rowOf(e *env, t *testing.T, name string) {
	t.Helper()
	e.click(t, name)
}

func TestProcessesInspectorShowsDetailsAndNoEnvironment(t *testing.T) {
	ps := []model.Process{
		{PID: 1, PPID: 0, Name: "launchd", StartTime: started},
		{PID: 4242, PPID: 1, Name: "python3", User: "alice", Command: "/usr/bin/python3 -m http.server 8765",
			StartTime: started, Threads: 6, Ports: []model.Port{{Number: 8765, Proto: "tcp"}}, AppBundle: "com.example.Server"},
	}
	e := newEnv(ps)
	e.search(t, "python")
	lacks(t, e.tt, "Terminate", "Force kill")
	rowOf(e, t, "python3")
	has(t, e.tt, "/usr/bin/python3 -m http.server 8765", "4242", "1 (launchd)", "alice", "6", "8765/tcp",
		"com.example.Server", "Terminate", "Force kill",
		started.In(time.Local).Format("2006-01-02 15:04:05")+" · "+format.Ago(3*time.Hour))
	lacks(t, e.tt, "Environment", "Env")
}

func TestProcessesInspectorFallsBackToNameWithoutCommand(t *testing.T) {
	e := newEnv([]model.Process{{PID: 9, PPID: 0, Name: "kernel_task", StartTime: started}})
	e.search(t, "kernel")
	// the name is the row and the command line fallback: two texts
	rowOf(e, t, "kernel_task")
	n := 0
	for _, x := range e.tt.Texts() {
		if x == "kernel_task" {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("name not shown as the command line: %q", e.tt.Texts())
	}
	lacks(t, e.tt, "App")
}

func startKill(t *testing.T, e *env) {
	t.Helper()
	e.search(t, "sleep")
	rowOf(e, t, "sleep")
}

func TestProcessesForceKillAsksTwice(t *testing.T) {
	e := newEnv(fixtureProcs(50))
	startKill(t, e)
	e.click(t, "Force kill")
	has(t, e.tt, "Force kill sleep (PID 4242)?")
	lacks(t, e.tt, "Really force kill sleep (PID 4242)?")
	if len(e.run.Started) != 0 {
		t.Fatalf("started before any confirmation: %+v", e.run.Started)
	}
	e.click(t, "Continue")
	has(t, e.tt, "Really force kill sleep (PID 4242)?")
	lacks(t, e.tt, "Force kill sleep (PID 4242)?")
	if len(e.run.Started) != 0 {
		t.Fatalf("started after one confirmation: %+v", e.run.Started)
	}
	e.click(t, "Force kill process")
	if len(e.run.Started) != 1 {
		t.Fatalf("started %+v", e.run.Started)
	}
	a := e.run.Started[0]
	if a.Verb != "KILL" || a.Kind != "signal" || a.HostID != "h1" || a.State != "confirming" ||
		a.Target != fmt.Sprintf("4242@%d", fixtureProcs(50)[0].StartTime.UnixNano()) {
		t.Fatalf("started %+v", a)
	}
}

func TestProcessesCancelStartsNothing(t *testing.T) {
	e := newEnv(fixtureProcs(50))
	startKill(t, e)
	e.click(t, "Force kill")
	e.click(t, "Cancel")
	e.click(t, "Terminate")
	e.click(t, "Cancel")
	if len(e.run.Started) != 0 {
		t.Fatalf("started %+v", e.run.Started)
	}
	e.tt.Key(0, ui.KeyEscape)
}

func TestProcessesButtonsDisabledWhileRunning(t *testing.T) {
	e := newEnv(fixtureProcs(50))
	startKill(t, e)
	e.click(t, "Terminate")
	e.click(t, "Terminate process")
	if len(e.run.Started) != 1 {
		t.Fatalf("started %+v", e.run.Started)
	}
	has(t, e.tt, "Terminate sleep (4242): running")
	e.click(t, "Terminate")
	e.click(t, "Force kill")
	lacks(t, e.tt, "Terminate sleep (PID 4242)?", "Force kill sleep (PID 4242)?")
	// The runner publishes the final state; a late older snapshot is dropped.
	id := e.run.Started[0].ID
	e.acts.Apply(hub.Action{ID: id, Kind: "signal", HostID: "h1", Target: e.run.Started[0].Target, State: "succeeded", Seq: 3})
	e.acts.Apply(hub.Action{ID: id, Kind: "signal", HostID: "h1", Target: e.run.Started[0].Target, State: "running", Seq: 2})
	e.tt.Frame()
	has(t, e.tt, "Terminate sleep (4242): succeeded")
	lacks(t, e.tt, "Terminate sleep (4242): running")
	e.click(t, "Force kill")
	has(t, e.tt, "Force kill sleep (PID 4242)?")
}

func TestProcessesShowsFailedActionMessage(t *testing.T) {
	e := newEnv(fixtureProcs(50))
	startKill(t, e)
	e.click(t, "Terminate")
	e.click(t, "Terminate process")
	a := e.run.Started[0]
	e.acts.Apply(hub.Action{ID: a.ID, Kind: "signal", HostID: "h1", Target: a.Target, State: "failed", Seq: 2,
		Err: &model.APIError{Code: "conflict", Message: "process start time changed"}})
	e.tt.Frame()
	has(t, e.tt, "Terminate sleep (4242): failed: process start time changed")
}

func TestProcessesErrBannerOnlyOnThisPage(t *testing.T) {
	e := newEnv(fixtureProcs(10))
	lacks(t, e.tt, "Process list may be stale: connection refused")
	e.view.ProcsErr = errors.New("connection refused")
	e.tt.Frame()
	has(t, e.tt, "Process list may be stale: connection refused")
	has(t, e.tt, "chrome") // the last good list stays
	// Other pages never show it.
	tt := render(pages.CPU, hub.HostView{Snap: snap(), Procs: procs(), ProcsErr: errors.New("connection refused")})
	lacks(t, tt, "Process list may be stale: connection refused")
}

func TestProcessesColumnsHiddenAndReorderedArePersisted(t *testing.T) {
	e := newEnv(fixtureProcs(20))
	if err := e.tt.RightClick("Threads"); err != nil {
		t.Fatal(err)
	}
	if m := e.tt.Menu(); !slices.Equal(m, []string{"Name", "CPU", "Memory", "Threads", "PID", "User", "Ports", "Started"}) {
		t.Fatalf("menu %q", m)
	}
	if err := e.tt.ChooseMenuItem("Threads"); err != nil {
		t.Fatal(err)
	}
	want := []string{"name", "cpu", "mem", "pid", "user", "ports", "started"}
	if len(e.cols) != 1 || !slices.Equal(e.cols[0], want) {
		t.Fatalf("hook calls %q, want [%q]", e.cols, want)
	}
	lacks(t, e.tt, "Threads")
	// Showing it again appends it.
	if err := e.tt.RightClick("Ports"); err != nil {
		t.Fatal(err)
	}
	if err := e.tt.ChooseMenuItem("Threads"); err != nil {
		t.Fatal(err)
	}
	want = append(want, "threads")
	if len(e.cols) != 2 || !slices.Equal(e.cols[1], want) {
		t.Fatalf("hook calls %q", e.cols)
	}
	has(t, e.tt, "Threads")

	// Dragging the Started header onto the PID header moves it.
	from, _ := e.tt.Find("Started")
	to, _ := e.tt.Find("PID")
	fx, fy := from.X+from.W/2, from.Y+from.H/2
	e.tt.Press(fx, fy)
	for x := fx; x > to.X; x -= 20 {
		e.tt.Move(x, fy)
	}
	e.tt.Move(to.X+2, fy)
	e.tt.Release(to.X+2, fy)
	if len(e.cols) < 3 {
		t.Fatalf("hook not called on reorder: %q", e.cols)
	}
	if got := e.cols[len(e.cols)-1]; slices.Index(got, "started") >= slices.Index(got, "pid") || len(got) != 8 {
		t.Fatalf("reordered columns %q", got)
	}
}

func TestProcessesLastColumnCannotBeHidden(t *testing.T) {
	e := newEnv(fixtureProcs(5))
	e.page.SetColumns([]string{"name"})
	e.tt.Frame()
	if err := e.tt.RightClick("Name"); err != nil {
		t.Fatal(err)
	}
	_ = e.tt.ChooseMenuItem("Name")
	if len(e.cols) != 0 {
		t.Fatalf("hid the last column: %q", e.cols)
	}
}

func TestProcessesSavedColumnsAreRestored(t *testing.T) {
	e := newEnv(fixtureProcs(5))
	e.page.SetColumns([]string{"pid", "bogus", "name", "pid"})
	e.tt.Frame()
	lacks(t, e.tt, "CPU", "User", "Memory")
	has(t, e.tt, "PID", "Name")
	before(t, e.tt, "PID", "Name")
	e.page.SetColumns(nil) // empty saved choice: all columns
	e.tt.Frame()
	has(t, e.tt, "CPU", "User", "Memory", "Threads", "Ports", "Started")
}

func TestActionsKeepHighestSeq(t *testing.T) {
	a := pages.NewActions(&hub.FakeRunner{})
	a.Apply(hub.Action{ID: 1, Kind: "signal", HostID: "h", Target: "1@1", State: "running", Seq: 1})
	a.Apply(hub.Action{ID: 1, Kind: "signal", HostID: "h", Target: "1@1", State: "succeeded", Seq: 3})
	a.Apply(hub.Action{ID: 1, Kind: "signal", HostID: "h", Target: "1@1", State: "running", Seq: 2})
	if got, _ := a.Last("h", "signal"); got.State != "succeeded" {
		t.Fatalf("last %+v", got)
	}
	if a.Running("h", "1@1") {
		t.Fatal("finished action reported running")
	}
	a.Apply(hub.Action{ID: 2, Kind: "signal", HostID: "h", Target: "1@1", State: "running", Seq: 1})
	if !a.Running("h", "1@1") || a.Running("other", "1@1") {
		t.Fatal("running not tracked per host and target")
	}
}

func TestAppApplyActionAndColumnsHook(t *testing.T) {
	procs := fixtureProcs(10)
	hosts := []store.Host{{ID: "h1", Name: "Mac", Kind: "local"}}
	runner := &hub.FakeRunner{}
	app := hwui.NewApp(func(string) hub.HostView { return hub.HostView{Procs: procs} }, hosts, runner, func(string, string) {})
	var page string
	var saved []string
	app.SetColumnsHook(func(p string, cols []string) { page, saved = p, cols })
	app.SetSavedColumns("processes", []string{"name", "pid"})
	tt := ui.NewTester(app.View, 1400, 900)
	if err := tt.Click("Processes"); err != nil {
		t.Fatal(err)
	}
	lacks(t, tt, "Threads", "Ports", "Started")
	if err := tt.RightClick("PID"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("User"); err != nil {
		t.Fatal(err)
	}
	if page != "processes" || !slices.Equal(saved, []string{"name", "pid", "user"}) {
		t.Fatalf("hook got %q %q", page, saved)
	}
	app.ApplyAction(hub.Action{ID: 5, Kind: "signal", HostID: "h1", Target: fmt.Sprintf("77@%d", procs[1].StartTime.UnixNano()),
		State: "failed", Seq: 4, Err: &model.APIError{Message: "denied"}})
	app.ApplyAction(hub.Action{ID: 5, Kind: "signal", HostID: "h1", State: "running", Seq: 1})
	tt.Frame()
	has(t, tt, "Terminate chrome (77): failed: denied")
}

// TestProcessesImage writes the page with a row selected for visual review.
func TestProcessesImage(t *testing.T) {
	out := os.Getenv("HOTWATCH_E5_PNG")
	if out == "" {
		t.Skip("set HOTWATCH_E5_PNG to render the page image")
	}
	ps := fixtureProcs(60)
	ps[1].Ports = []model.Port{{Number: 9222, Proto: "tcp"}}
	ps[1].Command = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome --remote-debugging-port=9222"
	ps[1].AppBundle = "com.google.Chrome"
	hosts := []store.Host{{ID: "h1", Name: "Mac", Kind: "local"}}
	app := hwui.NewApp(func(string) hub.HostView {
		return hub.HostView{Info: model.HostInfo{OS: "darwin"}, Procs: ps, Snap: snap(),
			Conn: hub.Conn{State: hub.Online, LastOK: time.Now()}}
	}, hosts, &hub.FakeRunner{}, func(string, string) {})
	tt := ui.NewTester(app.View, 1400, 900)
	if err := tt.Click("Processes"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("chrome"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// refreshes are the ways a process list changes under an open dialog.
var refreshes = map[string]func(ps []model.Process) []model.Process{
	"reordered": func(ps []model.Process) []model.Process {
		out := slices.Clone(ps)
		slices.Reverse(out)
		return out
	},
	"removed": func(ps []model.Process) []model.Process {
		return slices.DeleteFunc(slices.Clone(ps), func(p model.Process) bool { return p.PID == 4242 })
	},
	"pid reused": func(ps []model.Process) []model.Process {
		out := slices.Clone(ps)
		for i := range out {
			if out[i].PID == 4242 {
				out[i].StartTime = out[i].StartTime.Add(time.Hour)
			}
		}
		return out
	},
}

func TestProcessesConfirmedTargetIsPinnedAcrossRefresh(t *testing.T) {
	orig := fixtureProcs(50)
	wantTarget := fmt.Sprintf("4242@%d", orig[0].StartTime.UnixNano())
	for name, refresh := range refreshes {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/force=%v", name, force), func(t *testing.T) {
				e := newEnv(slices.Clone(orig))
				startKill(t, e)
				verb, last := "TERM", "Terminate process"
				if force {
					e.click(t, "Force kill")
					verb, last = "KILL", "Force kill process"
				} else {
					e.click(t, "Terminate")
				}
				e.view.Procs = refresh(orig)
				e.gen++
				e.tt.Frame()
				if force {
					e.click(t, "Continue")
					e.view.Procs = refresh(orig)
					e.gen++
					e.tt.Frame()
				}
				e.click(t, last)
				if len(e.run.Started) != 1 || e.run.Started[0].Target != wantTarget || e.run.Started[0].Verb != verb {
					t.Fatalf("started %+v, want %s %s", e.run.Started, verb, wantTarget)
				}
			})
		}
	}
}

func TestProcessesInspectorFollowsProcessAcrossRefresh(t *testing.T) {
	orig := fixtureProcs(20)
	e := newEnv(slices.Clone(orig))
	startKill(t, e)
	stamp := orig[0].StartTime.Local().Format("2006-01-02 15:04:05")
	e.view.Procs = append([]model.Process{proc(5, "newcomer", 99)}, orig...)
	e.gen++
	e.tt.Frame()
	has(t, e.tt, "4242", "Terminate")
	if !slices.ContainsFunc(e.tt.Texts(), func(x string) bool { return strings.HasPrefix(x, stamp) }) {
		t.Fatalf("inspector lost the start time: %q", e.tt.Texts())
	}
	e.view.Procs = slices.Clone(orig[1:])
	e.gen++
	e.tt.Frame()
	lacks(t, e.tt, "Terminate", "Force kill")
}

func TestProcessesUnknownStartTimeCannotBeSignalled(t *testing.T) {
	ps := []model.Process{{PID: 9, PPID: 0, Name: "mystery"}}
	e := newEnv(ps)
	rowOf(e, t, "mystery")
	has(t, e.tt, "Start time unknown — cannot signal safely", "Terminate", "Force kill")
	e.click(t, "Terminate")
	e.click(t, "Force kill")
	lacks(t, e.tt, "Terminate mystery (PID 9)?", "Force kill mystery (PID 9)?")
	if len(e.run.Started) != 0 {
		t.Fatalf("started %+v", e.run.Started)
	}
}

func TestProcessesFilteringOutTheSelectionClosesInspector(t *testing.T) {
	e := newEnv(fixtureProcs(20))
	startKill(t, e)
	has(t, e.tt, "Terminate")
	e.click(t, "Search")
	e.tt.Key(ui.Cmd, ui.KeyA)
	e.tt.Type("chrome")
	lacks(t, e.tt, "Terminate", "Force kill")
	e.tt.Key(ui.Cmd, ui.KeyA)
	e.tt.Type("sleep")
	lacks(t, e.tt, "Terminate") // the choice does not come back by itself
}

func treeProcs() []model.Process {
	return []model.Process{
		{PID: 1, PPID: 0, Name: "init", StartTime: started},
		{PID: 10, PPID: 1, Name: "shell", StartTime: started.Add(time.Second)},
		{PID: 11, PPID: 10, Name: "editor", StartTime: started.Add(2 * time.Second)},
	}
}

func TestProcessesTreeOpensNewParentsButKeepsCollapsed(t *testing.T) {
	e := newEnv(treeProcs())
	e.click(t, "Tree view")
	has(t, e.tt, "editor")
	// A parent that shows up in a refresh is open.
	e.view.Procs = append(treeProcs(),
		model.Process{PID: 20, PPID: 1, Name: "daemon", StartTime: started.Add(3 * time.Second)},
		model.Process{PID: 21, PPID: 20, Name: "worker", StartTime: started.Add(4 * time.Second)})
	e.gen++
	e.tt.Frame()
	has(t, e.tt, "worker")
	// The user's collapse of "shell" survives a refresh.
	collapse(e, "shell")
	lacks(t, e.tt, "editor")
	e.gen++
	e.tt.Frame()
	lacks(t, e.tt, "editor")
	// A filter that hides a parent and its clearing do not lose openness.
	e.search(t, "worker")
	e.tt.Key(ui.Cmd, ui.KeyA)
	e.tt.Type("")
	has(t, e.tt, "worker")
}

// collapse clicks the disclosure arrow left of the row showing name.
func collapse(e *env, name string) {
	r, _ := e.tt.Find(name)
	e.tt.ClickAt(r.X-10, r.Y+r.H/2)
}

func TestProcessesTreeCollapseNeverLeavesHiddenRowInInspector(t *testing.T) {
	e := newEnv(treeProcs())
	e.click(t, "Tree view")
	rowOf(e, t, "editor")
	has(t, e.tt, "Terminate")
	collapse(e, "shell") // the tree moves the choice to the closed item
	lacks(t, e.tt, "editor")
	has(t, e.tt, "Terminate", "10")
	lacks(t, e.tt, "11")
}
