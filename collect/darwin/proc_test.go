//go:build darwin

package darwin

import (
	"context"
	"errors"
	"hotwatch/internal/testenv"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"hotwatch/model"
)

const lsofFixture = `p100
cmDNSResponder
f8
tIPv4
PUDP
n*:5353
f9
tIPv6
PUDP
n*:5353
f10
tIPv4
PUDP
n192.168.1.2:5000->17.0.0.1:443
p200
cpython3
f4
tIPv4
PTCP
n127.0.0.1:8765
f5
tIPv6
PTCP
n[::1]:8765
f6
tIPv6
PTCP
n*:9000
p300
cweird
f3
tIPv4
PTCP
nbogus
`

func TestParseLsofPorts(t *testing.T) {
	got := parseLsofPorts([]byte(lsofFixture))
	want := map[int32][]model.Port{
		100: {{Number: 5353, Proto: "udp"}},
		200: {{Number: 8765, Proto: "tcp"}, {Number: 9000, Proto: "tcp"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLsofPorts = %v, want %v", got, want)
	}
	if len(parseLsofPorts(nil)) != 0 {
		t.Fatal("empty input must give empty map")
	}
}

func TestAppBundle(t *testing.T) {
	cases := map[string]string{
		"/Applications/Safari.app/Contents/MacOS/Safari":                     "/Applications/Safari.app",
		"/Applications/Foo.app/Contents/Frameworks/Bar.app/Contents/MacOS/x": "/Applications/Foo.app/Contents/Frameworks/Bar.app",
		"/usr/bin/python3": "",
		"":                 "",
	}
	for in, want := range cases {
		if got := appBundle(in); got != want {
			t.Errorf("appBundle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapCommand(t *testing.T) {
	s := strings.Repeat("é", 3000) // 6000 bytes
	got := capCommand(s)
	if len(got) > 4096 || !utf8.ValidString(got) {
		t.Fatalf("len=%d valid=%v", len(got), utf8.ValidString(got))
	}
	if capCommand("ls -l") != "ls -l" {
		t.Fatal("short command changed")
	}
}

func TestProcessesHTTPServer(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	before := time.Now()
	cmd := exec.Command("python3", "-m", "http.server", "8765", "--bind", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	for i := 0; ; i++ {
		c, err := net.DialTimeout("tcp", "127.0.0.1:8765", 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if i > 50 {
			t.Fatal("server did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	col := New().(*collector)
	procs, err := col.Processes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found, launchd bool
	for _, p := range procs {
		switch p.PID {
		case int32(cmd.Process.Pid):
			found = true
			if d := p.StartTime.Sub(before); d < -2*time.Second || d > 5*time.Second {
				t.Errorf("StartTime %v not near %v", p.StartTime, before)
			}
			has := false
			for _, pt := range p.Ports {
				if pt == (model.Port{Number: 8765, Proto: "tcp"}) {
					has = true
				}
			}
			if !has {
				t.Errorf("ports = %v, want 8765/tcp", p.Ports)
			}
			if p.PPID != int32(os.Getpid()) || p.RSS == 0 || p.Threads == 0 || !strings.Contains(p.Command, "http.server") {
				t.Errorf("bad fields: %+v", p)
			}
		case 1:
			launchd = true
			if os.Geteuid() != 0 && (p.DiskReadBps != nil || p.DiskWriteBps != nil) {
				t.Errorf("launchd disk rates must be nil for non-root")
			}
			if p.StartTime.IsZero() {
				t.Errorf("launchd StartTime zero")
			}
		}
	}
	if !found || !launchd {
		t.Fatalf("found server=%v launchd=%v among %d", found, launchd, len(procs))
	}
}

func TestProcessesSelfRatesAfterSecondCall(t *testing.T) {
	col := New().(*collector)
	t0 := time.Unix(1000, 0)
	col.now = func() time.Time { return t0 }
	find := func() model.Process {
		ps, err := col.Processes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.PID == int32(os.Getpid()) {
				return p
			}
		}
		t.Fatal("self not found")
		return model.Process{}
	}
	first := find()
	if first.DiskReadBps != nil || first.DiskWriteBps != nil {
		t.Error("first sight must have nil rates")
	}
	f, _ := os.CreateTemp(t.TempDir(), "w")
	buf := make([]byte, 1<<20)
	for i := 0; i < 8; i++ {
		f.Write(buf)
	}
	f.Sync()
	t0 = t0.Add(2 * time.Second)
	second := find()
	if second.DiskReadBps == nil || second.DiskWriteBps == nil {
		t.Fatal("second sample must have rates for own process")
	}
}

func BenchmarkProcesses(b *testing.B) {
	col := New().(*collector)
	ctx := context.Background()
	col.Processes(ctx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := col.Processes(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

const fakeLsof = "p42\nf1\ntIPv4\nPTCP\nn*:1234\n"

type fakeRunner struct {
	mu    sync.Mutex
	runs  int
	cur   int
	maxC  int
	delay time.Duration
	fail  bool
}

func (f *fakeRunner) run(context.Context) ([]byte, error) {
	f.mu.Lock()
	f.runs++
	f.cur++
	if f.cur > f.maxC {
		f.maxC = f.cur
	}
	delay, fail := f.delay, f.fail
	f.mu.Unlock()
	time.Sleep(delay)
	f.mu.Lock()
	f.cur--
	f.mu.Unlock()
	if fail {
		return nil, errors.New("lsof failed")
	}
	return []byte(fakeLsof), nil
}

func (f *fakeRunner) set(fn func()) { f.mu.Lock(); fn(); f.mu.Unlock() }
func (f *fakeRunner) count() int    { f.mu.Lock(); defer f.mu.Unlock(); return f.runs }

var want42 = map[int32][]model.Port{42: {{Number: 1234, Proto: "tcp"}}}

func TestPortsCacheWithinIntervalNoSecondRun(t *testing.T) {
	f := &fakeRunner{}
	c := &collector{lsof: f.run}
	t0 := time.Unix(1000, 0)
	if got := c.listeningPorts(context.Background(), t0); !reflect.DeepEqual(got, want42) {
		t.Fatalf("first = %v", got)
	}
	c.listeningPorts(context.Background(), t0.Add(2*time.Second))
	c.listeningPorts(context.Background(), t0.Add(9*time.Second))
	time.Sleep(50 * time.Millisecond)
	if n := f.count(); n != 1 {
		t.Fatalf("runs = %d, want 1", n)
	}
}

func TestPortsSlowRefreshDoesNotBlockAndSingleFlight(t *testing.T) {
	f := &fakeRunner{}
	c := &collector{lsof: f.run}
	t0 := time.Unix(1000, 0)
	c.listeningPorts(context.Background(), t0)
	f.set(func() { f.delay = 300 * time.Millisecond })
	start := time.Now()
	for i := 0; i < 5; i++ {
		got := c.listeningPorts(context.Background(), t0.Add(11*time.Second+time.Duration(i)*time.Millisecond))
		if !reflect.DeepEqual(got, want42) {
			t.Fatalf("stale ports lost: %v", got)
		}
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("calls blocked for %v", d)
	}
	time.Sleep(450 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runs != 2 || f.maxC != 1 {
		t.Fatalf("runs=%d maxConcurrent=%d, want 2 and 1", f.runs, f.maxC)
	}
}

func TestPortsFailureKeepsLastGoodThenExpires(t *testing.T) {
	f := &fakeRunner{}
	c := &collector{lsof: f.run}
	t0 := time.Unix(1000, 0)
	c.listeningPorts(context.Background(), t0)
	f.set(func() { f.fail = true })
	c.listeningPorts(context.Background(), t0.Add(11*time.Second)) // starts failing refresh
	time.Sleep(50 * time.Millisecond)
	if got := c.listeningPorts(context.Background(), t0.Add(12*time.Second)); !reflect.DeepEqual(got, want42) {
		t.Fatalf("good map lost after failure: %v", got)
	}
	if n := f.count(); n != 2 {
		t.Fatalf("runs = %d, want 2 (no retry before interval)", n)
	}
	if got := c.listeningPorts(context.Background(), t0.Add(61*time.Second)); len(got) != 0 {
		t.Fatalf("map older than 60s must be dropped, got %v", got)
	}
}

func TestPortsRefreshIgnoresCallerContext(t *testing.T) {
	f := &fakeRunner{}
	c := &collector{lsof: func(ctx context.Context) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return f.run(ctx)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := c.listeningPorts(ctx, time.Unix(1000, 0)); !reflect.DeepEqual(got, want42) {
		t.Fatalf("cancelled caller ctx blanked ports: %v", got)
	}
}

func TestProcessesReadStaticFactsOncePerProcess(t *testing.T) {
	c := New().(*collector)
	ctx := context.Background()
	first, err := c.Processes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := staticReads.Load()
	second, err := c.Processes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reread := staticReads.Load() - before
	// Only processes started between the two scans are read in full.
	if reread > int64(len(second)/10) {
		t.Fatalf("second scan re-read %d of %d processes' static facts", reread, len(second))
	}
	byKey := map[int32]model.Process{}
	for _, p := range first {
		byKey[p.PID] = p
	}
	same := 0
	for _, p := range second {
		if q, ok := byKey[p.PID]; ok && q.StartTime.Equal(p.StartTime) {
			if q.Name != p.Name || q.User != p.User || q.Command != p.Command || q.AppBundle != p.AppBundle {
				t.Fatalf("cached facts differ for %d: %+v vs %+v", p.PID, q, p)
			}
			same++
		}
	}
	if same < len(second)/2 {
		t.Fatalf("only %d of %d processes matched between scans", same, len(second))
	}
}

func TestProcessesNoticeAnExecUnderTheSamePID(t *testing.T) {
	// sh replaces itself with sleep: same PID, same start time, new program.
	cmd := exec.Command("/bin/sh", "-c", "/bin/sleep 0.4; exec /bin/sleep 5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	c := New().(*collector)
	find := func() model.Process {
		ps, err := c.Processes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.PID == int32(cmd.Process.Pid) {
				return p
			}
		}
		t.Fatalf("pid %d not listed", cmd.Process.Pid)
		return model.Process{}
	}
	if p := find(); p.Name == "sleep" {
		t.Fatalf("before exec: %q", p.Name)
	}
	time.Sleep(800 * time.Millisecond)
	p := find()
	if p.Name != "sleep" || !strings.Contains(p.Command, "sleep 5") {
		t.Fatalf("after exec the cache still says %q / %q", p.Name, p.Command)
	}
}

func TestOwnProcessHasAFootprint(t *testing.T) {
	ps, err := New().Processes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.PID == int32(os.Getpid()) {
			if p.Footprint == 0 || p.Footprint > 64<<30 {
				t.Fatalf("own footprint %d (RSS %d)", p.Footprint, p.RSS)
			}
			return
		}
	}
	t.Fatal("own process not listed")
}

func TestBusyProcessHasPower(t *testing.T) {
	testenv.RequireHardware(t)
	c := New().(*collector)
	ctx := context.Background()
	if _, err := c.Processes(ctx); err != nil {
		t.Fatal(err)
	}
	end := time.Now().Add(700 * time.Millisecond)
	for x := 0; time.Now().Before(end); x++ { // keep a core busy
		_ = x * x
	}
	ps, err := c.Processes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.PID == int32(os.Getpid()) {
			if p.PowerMW == nil || *p.PowerMW <= 0 || *p.PowerMW > 50000 {
				if p.PowerMW != nil {
					t.Fatalf("own power %v mW", *p.PowerMW)
				}
				t.Fatal("own power unknown")
			}
			t.Logf("busy test process: %.0f mW", *p.PowerMW)
			return
		}
	}
	t.Fatal("own process not listed")
}
