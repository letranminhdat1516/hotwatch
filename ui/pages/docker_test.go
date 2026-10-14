package pages_test

import (
	"fmt"
	"image/png"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	hwui "hotwatch/ui"
	"hotwatch/ui/components"
	"hotwatch/ui/pages"
)

func ctr(id, name, project, state string) model.Container {
	c := model.Container{ID: id, Name: name, Image: name + ":latest", State: state, Project: project}
	if state == "running" {
		c.StartedAt = started
	}
	return c
}

func fixtureCtrs() []model.Container {
	bot := ctr("c1", "jobhunter-bot", "jobhunter", "running")
	cpu, used, limit := 12.5, uint64(256<<20), uint64(1<<30)
	bot.CPUPct, bot.MemUsed, bot.MemLimit = &cpu, &used, &limit
	bot.Ports = []string{"8080/tcp"}
	bot.Health = "healthy"
	db := ctr("c2", "jobhunter-db", "jobhunter", "exited")
	web := ctr("c3", "alpha-web", "alpha", "restarting")
	solo := ctr("c4", "standalone", "", "paused")
	mk := ctr("c5", "zeta-new", "zeta", "created")
	return []model.Container{solo, db, bot, mk, web}
}

// fakeLogs is a LogsOpener that records each stream it opens.
type fakeLogs struct {
	mu      sync.Mutex
	streams []*fakeStream
	err     error
	buf     int // channel capacity of new streams
}

type fakeStream struct {
	host, id string
	tail     int
	follow   bool
	ch       chan model.LogLine
	cancels  int
}

func (f *fakeLogs) open(host, id string, tail int, follow bool) (<-chan model.LogLine, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, nil, f.err
	}
	s := &fakeStream{host: host, id: id, tail: tail, follow: follow, ch: make(chan model.LogLine, f.buf)}
	f.streams = append(f.streams, s)
	return s.ch, func() { f.mu.Lock(); s.cancels++; f.mu.Unlock() }, nil
}

func (f *fakeLogs) last() *fakeStream {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.streams) == 0 {
		return nil
	}
	return f.streams[len(f.streams)-1]
}

func (f *fakeLogs) cancels(i int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streams[i].cancels
}

func (f *fakeLogs) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.streams)
}

type denv struct {
	tt   *ui.Tester
	page *pages.Docker
	run  *hub.FakeRunner
	acts *pages.Actions
	logs *fakeLogs
	view hub.HostView
	gen  uint64
	host string
}

func newDenv(ctrs []model.Container) *denv {
	e := &denv{page: pages.NewDocker(), run: &hub.FakeRunner{}, logs: &fakeLogs{}, host: "h1",
		view: hub.HostView{Ctrs: ctrs, Info: model.HostInfo{Caps: model.Caps{Docker: true}}}}
	e.acts = pages.NewActions(e.run)
	e.page.SetLogsOpener(e.logs.open)
	e.page.SetPost(func(f func()) { f() })
	e.tt = ui.NewTester(func(c *ui.Context) {
		c.SetTheme(hwui.Theme(false))
		e.page.Render(c, pages.DockerInput{HostID: e.host, View: e.view, Gen: e.gen, Now: started.Add(3*time.Hour + 5*time.Minute),
			Actions: e.acts})
	}, 1400, 900)
	return e
}

func (e *denv) click(t *testing.T, s string) {
	t.Helper()
	if err := e.tt.Click(s); err != nil {
		t.Fatal(err)
	}
}

// ready waits until n streams were opened and the last one is connected.
func (e *denv) ready(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if e.logs.count() >= n && !e.page.LogsConnecting() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("stream %d never connected", n)
}

func (e *denv) refresh() { e.gen++; e.tt.Frame() }

// send delivers a line on the open stream and waits for it to show.
func (e *denv) send(t *testing.T, l model.LogLine) {
	t.Helper()
	s := e.logs.last()
	select {
	case s.ch <- l:
	case <-time.After(2 * time.Second):
		t.Fatal("stream not reading")
	}
}

func (e *denv) waitText(t *testing.T, s string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		e.tt.Frame()
		if slices.Contains(e.tt.Texts(), s) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("text %q never shown in %q", s, e.tt.Texts())
}

func ll(sec int, stream, text string) model.LogLine {
	return model.LogLine{Time: time.Date(2026, 10, 15, 9, 30, sec, 0, time.Local), Stream: stream, Text: text}
}

func TestDockerShowsRowsGroupedByProject(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.tt.Frame()
	order := []string{"alpha", "jobhunter", "zeta", "No project"}
	last := -1
	for _, h := range order {
		i := pos(e.tt, h)
		if i < 0 || i <= last {
			t.Fatalf("project header %q at %d (after %d) in %q", h, i, last, e.tt.Texts())
		}
		last = i
	}
	before(t, e.tt, "jobhunter-bot", "standalone")
	before(t, e.tt, "jobhunter-bot", "jobhunter-db")
	for _, want := range []string{"Running", "Exited", "Restarting", "Paused", "Created", "healthy", "12.5 %",
		"256.0 MB / 1.0 GB", "8080/tcp", "3h 5m"} {
		if pos(e.tt, want) < 0 {
			t.Fatalf("missing %q in %q", want, e.tt.Texts())
		}
	}
}

func TestDockerUnknownValuesAreDashesAndUptimeOnlyWhileRunning(t *testing.T) {
	exited := ctr("c9", "old", "p", "exited")
	exited.StartedAt = started // stale start time of a stopped container
	e := newDenv([]model.Container{exited})
	e.tt.Frame()
	n := 0
	for _, s := range e.tt.Texts() {
		if s == "—" {
			n++
		}
	}
	if n != 5 { // health, cpu, memory, ports, uptime
		t.Fatalf("%d dashes in %q", n, e.tt.Texts())
	}
	if pos(e.tt, "3h 5m") >= 0 {
		t.Fatal("uptime shown for a stopped container")
	}
}

func TestDockerStateTones(t *testing.T) {
	th := hwui.Theme(false)
	for state, want := range map[string]ui.Color{"running": th.Success, "restarting": th.Warning, "paused": th.Warning,
		"exited": th.Danger, "dead": th.Danger, "created": th.TextMuted} {
		if got := components.StateColor(th, state); got != want {
			t.Fatalf("%s: %v, want %v", state, got, want)
		}
	}
}

func TestDockerActionButtonsFollowState(t *testing.T) {
	run := newDenv([]model.Container{ctr("c1", "up", "p", "running")})
	run.tt.Frame()
	for s, want := range map[string]bool{"Start": false, "Stop": true, "Restart": true, "Logs": true} {
		if got := pos(run.tt, s) >= 0; got != want {
			t.Fatalf("running: %q shown=%v", s, got)
		}
	}
	down := newDenv([]model.Container{ctr("c2", "down", "p", "exited")})
	down.tt.Frame()
	for s, want := range map[string]bool{"Start": true, "Stop": false, "Restart": false, "Logs": true} {
		if got := pos(down.tt, s) >= 0; got != want {
			t.Fatalf("exited: %q shown=%v", s, got)
		}
	}
}

func TestDockerRestartConfirmsThenRuns(t *testing.T) {
	e := newDenv([]model.Container{ctr("c1", "jobhunter-bot", "p", "running")})
	e.tt.Frame()
	e.click(t, "Restart")
	if len(e.run.Started) != 0 {
		t.Fatal("started before confirmation")
	}
	if pos(e.tt, "Restart jobhunter-bot?") < 0 {
		t.Fatalf("dialog does not name the container: %q", e.tt.Texts())
	}
	e.click(t, "Restart container")
	if len(e.run.Started) != 1 {
		t.Fatalf("started %+v", e.run.Started)
	}
	want := hub.Action{ID: 1, Kind: "container", HostID: "h1", Verb: "restart", State: "confirming", Target: "c1"}
	if fmt.Sprint(e.run.Started[0]) != fmt.Sprint(want) {
		t.Fatalf("started %+v, want %+v", e.run.Started[0], want)
	}
}

func TestDockerCancelStartsNothing(t *testing.T) {
	e := newDenv([]model.Container{ctr("c1", "up", "p", "running")})
	e.tt.Frame()
	e.click(t, "Stop")
	e.click(t, "Cancel")
	if len(e.run.Started) != 0 {
		t.Fatalf("started %+v", e.run.Started)
	}
}

func TestDockerPinnedTargetSurvivesRefresh(t *testing.T) {
	e := newDenv([]model.Container{ctr("c1", "alpha", "p", "running"), ctr("c2", "beta", "p", "running")})
	e.tt.Frame()
	e.click(t, "Stop") // first row: alpha
	// The list reorders and the host changes under the open dialog.
	e.view.Ctrs = []model.Container{ctr("c2", "beta", "p", "running"), ctr("c1", "alpha", "p", "running")}
	e.refresh()
	if pos(e.tt, "Stop alpha?") < 0 {
		t.Fatalf("dialog lost its container: %q", e.tt.Texts())
	}
	e.click(t, "Stop container")
	if len(e.run.Started) != 1 || e.run.Started[0].Target != "c1" || e.run.Started[0].Verb != "stop" {
		t.Fatalf("started %+v", e.run.Started)
	}
}

func TestDockerButtonsDisabledWhileActionRunsAndResultShown(t *testing.T) {
	e := newDenv([]model.Container{ctr("c1", "up", "p", "running")})
	e.tt.Frame()
	e.click(t, "Restart")
	e.click(t, "Restart container")
	e.tt.Frame()
	n := len(e.run.Started)
	// A second click while the action runs starts nothing.
	_ = e.tt.Click("Restart")
	_ = e.tt.Click("Stop")
	_ = e.tt.Click("Stop container")
	if len(e.run.Started) != n {
		t.Fatalf("duplicate action started: %+v", e.run.Started)
	}
	if pos(e.tt, "Restart: running") < 0 {
		t.Fatalf("no inline status in %q", e.tt.Texts())
	}
	e.acts.Apply(hub.Action{ID: 1, Kind: "container", HostID: "h1", Target: "c1", Verb: "restart", State: "failed", Seq: 5,
		Err: &model.APIError{Code: "conflict", Message: "boom"}})
	e.tt.Frame()
	if pos(e.tt, "Restart: failed: boom") < 0 {
		t.Fatalf("result not shown in %q", e.tt.Texts())
	}
}

func TestDockerLogsShowLinesAndAppend(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.tt.Frame()
	e.click(t, "Logs") // alpha-web, first row
	e.ready(t, 1)
	s := e.logs.last()
	if s == nil || s.id != "c3" || s.host != "h1" || s.tail != 100 || !s.follow {
		t.Fatalf("stream %+v", s)
	}
	e.send(t, ll(1, "stdout", "hello world"))
	e.waitText(t, "hello world")
	if pos(e.tt, "09:30:01") < 0 && !slices.Contains(e.tt.Texts(), time.Date(2026, 10, 15, 9, 30, 1, 0, time.Local).Local().Format("15:04:05")) {
		t.Fatalf("no timestamp in %q", e.tt.Texts())
	}
	e.send(t, ll(2, "stderr", "second line"))
	e.waitText(t, "second line")
	for _, want := range []string{"alpha-web", "Following", "Clear", "100", "500", "1000"} {
		if pos(e.tt, want) < 0 {
			t.Fatalf("pane misses %q in %q", want, e.tt.Texts())
		}
	}
}

func TestDockerLogsPlainTextAndControlCharsReplaced(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.tt.Frame()
	e.click(t, "Logs")
	e.ready(t, 1)
	e.send(t, ll(1, "stdout", "<b>bold</b> \x1b[31mred\x00\x07"))
	e.waitText(t, "<b>bold</b> �[31mred��")
}

func TestDockerLogsCancelOnClose(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.tt.Frame()
	e.click(t, "Logs")
	e.ready(t, 1)
	e.click(t, "Close")
	if got := e.logs.cancels(0); got != 1 {
		t.Fatalf("cancel called %d times", got)
	}
	// The reader goroutine is gone: nobody takes a line any more.
	select {
	case e.logs.last().ch <- ll(1, "stdout", "late"):
		t.Fatal("reader still running after close")
	case <-time.After(100 * time.Millisecond):
	}
	if pos(e.tt, "Following") >= 0 {
		t.Fatal("pane still open")
	}
}

func TestDockerLogsCancelOnSwitchContainerHostAndPage(t *testing.T) {
	e := newDenv([]model.Container{ctr("c1", "aa", "p", "running"), ctr("c2", "bb", "p", "running")})
	e.tt.Frame()
	e.click(t, "Logs") // aa
	e.ready(t, 1)
	e.page.OpenLogs("c2")
	e.ready(t, 2)
	if e.logs.count() < 2 || e.logs.cancels(0) < 1 {
		t.Fatalf("streams %d, first cancelled %d", e.logs.count(), e.logs.cancels(0))
	}
	last := e.logs.count() - 1
	if e.logs.last().id != "c2" {
		t.Fatalf("open stream is for %q", e.logs.last().id)
	}
	// Switch host.
	e.host = "h2"
	e.refresh()
	if e.logs.cancels(last) != 1 {
		t.Fatalf("host switch: cancelled %d", e.logs.cancels(last))
	}
	// Page switch.
	e.host = "h1"
	e.refresh()
	e.page.OpenLogs("c1")
	e.ready(t, 3)
	n := e.logs.count() - 1
	e.page.CloseLogs()
	if e.logs.cancels(n) != 1 {
		t.Fatalf("page switch: cancelled %d", e.logs.cancels(n))
	}
	// Idempotent.
	e.page.CloseLogs()
	if e.logs.cancels(n) != 1 {
		t.Fatalf("cancel called again: %d", e.logs.cancels(n))
	}
}

func TestDockerLogsTailAndFollowReopen(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.tt.Frame()
	e.click(t, "Logs")
	e.ready(t, 1)
	e.send(t, ll(1, "stdout", "old"))
	e.waitText(t, "old")
	e.click(t, "500")
	e.tt.Frame()
	e.ready(t, 2)
	if e.logs.count() != 2 || e.logs.cancels(0) != 1 || e.logs.last().tail != 500 {
		t.Fatalf("tail: streams=%d cancels=%d last=%+v", e.logs.count(), e.logs.cancels(0), e.logs.last())
	}
	if pos(e.tt, "old") >= 0 {
		t.Fatal("old lines kept after tail change")
	}
	e.click(t, "Following")
	e.tt.Frame()
	e.ready(t, 3)
	if e.logs.count() != 3 || e.logs.cancels(1) != 1 || e.logs.last().follow {
		t.Fatalf("follow: streams=%d cancels=%d last=%+v", e.logs.count(), e.logs.cancels(1), e.logs.last())
	}
}

func TestDockerLogsClear(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.tt.Frame()
	e.click(t, "Logs")
	e.ready(t, 1)
	e.send(t, ll(1, "stdout", "gone soon"))
	e.waitText(t, "gone soon")
	e.click(t, "Clear")
	e.tt.Frame()
	if pos(e.tt, "gone soon") >= 0 || e.page.LogLen() != 0 {
		t.Fatalf("not cleared: %q", e.tt.Texts())
	}
	if e.logs.count() != 1 {
		t.Fatal("clear reopened the stream")
	}
}

func TestDockerLogsRingCappedAt5000(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.logs.buf = 6000
	ended := make(chan struct{})
	var once sync.Once
	e.page.SetPost(func(f func()) {
		f()
		if e.page.LogsEnded() {
			once.Do(func() { close(ended) })
		}
	})
	e.tt.Frame()
	e.click(t, "Logs")
	e.ready(t, 1)
	s := e.logs.last()
	for i := 0; i < 5100; i++ {
		s.ch <- ll(1, "stdout", fmt.Sprintf("line-%d", i))
	}
	close(s.ch)
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("stream never ended")
	}
	if n := e.page.LogLen(); n != 5000 {
		t.Fatalf("ring holds %d lines", n)
	}
	if first := e.page.LogLineAt(0).Text; first != "line-100" {
		t.Fatalf("oldest line %q", first)
	}
	if last := e.page.LogLineAt(4999).Text; last != "line-5099" {
		t.Fatalf("newest line %q", last)
	}
}

func TestDockerLogsHostileLineStaysBounded(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.tt.Frame()
	e.click(t, "Logs")
	e.ready(t, 1)
	e.send(t, ll(1, "stdout", strings.Repeat("x", 1<<20)))
	e.send(t, ll(2, "stdout", "after"))
	e.send(t, ll(3, "stdout", "after2"))
	if got := len(e.page.LogLineAt(0).Text); got > 20000 {
		t.Fatalf("line kept at %d bytes", got)
	}
}

func TestDockerLogsOpenError(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.logs.err = fmt.Errorf("agent offline")
	e.tt.Frame()
	e.click(t, "Logs")
	e.ready(t, 0)
	e.waitText(t, "Could not open logs: agent offline")
	if pos(e.tt, "Could not open logs: agent offline") < 0 {
		t.Fatalf("error not shown: %q", e.tt.Texts())
	}
}

func TestDockerStderrTint(t *testing.T) {
	th := hwui.Theme(false)
	if pages.LogTint(th, "stderr") != th.Danger {
		t.Fatal("stderr is not tinted with the Danger token")
	}
	if pages.LogTint(th, "stdout") != th.Text {
		t.Fatal("stdout is tinted")
	}
}

func TestDockerEmptyStates(t *testing.T) {
	e := newDenv(nil)
	e.view.Info.Caps.Docker = false
	e.tt.Frame()
	if pos(e.tt, "Docker not available on this host") < 0 {
		t.Fatalf("texts %q", e.tt.Texts())
	}
	e.view.CtrsErr = &model.APIError{Code: "not_found", Message: "Docker is not monitored on this Mac"}
	e.refresh()
	if pos(e.tt, "Docker is not monitored on this Mac") < 0 || pos(e.tt, "Docker not available on this host") >= 0 {
		t.Fatalf("texts %q", e.tt.Texts())
	}
}

func TestDockerErrorIsStaleBannerKeepingRows(t *testing.T) {
	e := newDenv(fixtureCtrs())
	e.view.CtrsErr = fmt.Errorf("timeout")
	e.tt.Frame()
	if pos(e.tt, "Containers may be stale: timeout") < 0 || pos(e.tt, "jobhunter-bot") < 0 {
		t.Fatalf("texts %q", e.tt.Texts())
	}
}

func TestDockerNoContainers(t *testing.T) {
	e := newDenv(nil)
	e.tt.Frame()
	if pos(e.tt, "No containers") < 0 {
		t.Fatalf("texts %q", e.tt.Texts())
	}
}

func TestAppDockerPageCancelsLogsOnPageSwitch(t *testing.T) {
	logs := &fakeLogs{}
	hosts := []store.Host{{ID: "h1", Name: "Mac", Kind: "local"}}
	v := hub.HostView{Ctrs: fixtureCtrs(), Info: model.HostInfo{Caps: model.Caps{Docker: true, Processes: true}}}
	app := hwui.NewApp(func(string) hub.HostView { return v }, hosts, &hub.FakeRunner{}, func(string, string) {})
	app.SetLogsOpener(logs.open)
	app.SetPost(func(f func()) { f() })
	tt := ui.NewTester(app.View, 1400, 900)
	if err := tt.Click("Docker"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2000 && logs.count() < 1; i++ {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // the opener's result is delivered
	if logs.count() != 1 {
		t.Fatalf("streams %d", logs.count())
	}
	if err := tt.Click("Processes"); err != nil {
		t.Fatal(err)
	}
	if logs.cancels(0) != 1 {
		t.Fatalf("cancelled %d times on page switch", logs.cancels(0))
	}
}

// TestDockerImage renders the page with its logs pane open.
func TestDockerImage(t *testing.T) {
	path := os.Getenv("HOTWATCH_E6_PNG")
	if path == "" {
		t.Skip("set HOTWATCH_E6_PNG to render the docker page")
	}
	e := newDenv(fixtureCtrs())
	e.tt.Frame()
	e.click(t, "Logs")
	e.ready(t, 1)
	for i := 0; i < 14; i++ {
		stream := "stdout"
		if i%5 == 4 {
			stream = "stderr"
		}
		e.send(t, ll(i, stream, fmt.Sprintf("worker %d: processed batch, queue depth %d <ok>", i, 40-i)))
	}
	e.waitText(t, "worker 13: processed batch, queue depth 27 <ok>")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, e.tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestCleanTextIsBoundedAfterExpansion(t *testing.T) {
	max := 16<<10 + len("…")
	for name, in := range map[string]string{
		"nul":  strings.Repeat("\x00", 16<<10),
		"ff":   strings.Repeat("\xff", 16<<10),
		"huge": strings.Repeat("\xff", 1<<20),
		"wide": strings.Repeat("é", 1<<20),
	} {
		got := pages.CleanText(in)
		if len(got) > max || !utf8.ValidString(got) {
			t.Fatalf("%s: %d bytes, valid=%v", name, len(got), utf8.ValidString(got))
		}
	}
	if got := pages.CleanText(strings.Repeat("é", 1<<20)); !strings.HasSuffix(got, "é…") {
		t.Fatal("not cut on a rune boundary")
	}
}

func TestCleanTextNeutralisesBidiAndBOM(t *testing.T) {
	in := "a\u202eb\u2066c\u200fd\u061ce\ufeff"
	if got, want := pages.CleanText(in), "a\uFFFDb\uFFFDc\uFFFDd\uFFFDe\uFFFD"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestDockerButtonsForRestartingAndPausedRows(t *testing.T) {
	for state, want := range map[string]map[string]bool{
		"restarting": {"Start": false, "Stop": true, "Restart": true},
		"paused":     {"Start": false, "Stop": true, "Restart": false},
		"dead":       {"Start": true, "Stop": false, "Restart": false},
	} {
		e := newDenv([]model.Container{ctr("c1", "x", "p", state)})
		e.tt.Frame()
		for s, w := range want {
			if got := pos(e.tt, s) >= 0; got != w {
				t.Fatalf("%s: %q shown=%v", state, s, got)
			}
		}
	}
}

func TestDockerSlowOpenerDoesNotBlockAndStaleResultIsCancelled(t *testing.T) {
	e := newDenv(fixtureCtrs())
	release := make(chan struct{})
	called := make(chan struct{})
	var mu sync.Mutex
	cancelled := 0
	e.page.SetLogsOpener(func(string, string, int, bool) (<-chan model.LogLine, func(), error) {
		close(called)
		<-release
		return make(chan model.LogLine), func() { mu.Lock(); cancelled++; mu.Unlock() }, nil
	})
	e.tt.Frame()
	start := time.Now()
	e.click(t, "Logs")
	<-called
	e.tt.Frame()
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("render blocked %v", d)
	}
	if pos(e.tt, "Connecting…") < 0 {
		t.Fatalf("no connecting note: %q", e.tt.Texts())
	}
	e.page.CloseLogs() // leave before the opener returns
	close(release)
	for i := 0; i < 2000; i++ {
		mu.Lock()
		n := cancelled
		mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("late open result was not cancelled")
}

func TestDockerLogsContainerLeavesList(t *testing.T) {
	e := newDenv([]model.Container{ctr("c1", "aa", "p", "running"), ctr("c2", "bb", "p", "running")})
	e.tt.Frame()
	e.click(t, "Logs") // aa
	e.ready(t, 1)
	e.view.Ctrs = []model.Container{ctr("c2", "bb", "p", "running")}
	e.refresh()
	if pos(e.tt, "Container no longer listed") < 0 || pos(e.tt, "aa") < 0 {
		t.Fatalf("texts %q", e.tt.Texts())
	}
	if e.logs.cancels(0) != 1 {
		t.Fatalf("cancelled %d", e.logs.cancels(0))
	}
}
