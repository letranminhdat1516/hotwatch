package hub

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"hotwatch/model"
	"hotwatch/source"
	"hotwatch/transport"
)

// fakeClock is a manually advanced clock. Advance fires timers in deadline
// order and lets woken goroutines run between firings.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c    chan time.Time
	at   time.Time
	dead bool
	clk  *fakeClock
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }
func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	was := !t.dead
	t.dead = true
	return was
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_000_000, 0)} }

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) NewTimer(d time.Duration) timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: make(chan time.Time, 1), at: c.now.Add(d), clk: c}
	c.timers = append(c.timers, t)
	return t
}

func settle() { time.Sleep(4 * time.Millisecond) }

func (c *fakeClock) Advance(d time.Duration) {
	settle()
	target := c.Now().Add(d)
	for {
		c.mu.Lock()
		var live []*fakeTimer
		for _, t := range c.timers {
			if !t.dead {
				live = append(live, t)
			}
		}
		c.timers = live
		sort.Slice(live, func(i, j int) bool { return live[i].at.Before(live[j].at) })
		if len(live) == 0 || live[0].at.After(target) {
			c.now = target
			c.mu.Unlock()
			settle()
			return
		}
		t := live[0]
		t.dead = true
		if t.at.After(c.now) {
			c.now = t.at
		}
		now := c.now
		c.mu.Unlock()
		t.c <- now
		settle()
	}
}

type counting struct {
	*source.Fake
	mu                      sync.Mutex
	info, snap, procs, ctrs int
}

func (c *counting) bump(p *int) { c.mu.Lock(); *p++; c.mu.Unlock() }
func (c *counting) counts() (info, snap, procs, ctrs int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info, c.snap, c.procs, c.ctrs
}
func (c *counting) Info(ctx context.Context) (model.HostInfo, error) {
	c.bump(&c.info)
	return c.Fake.Info(ctx)
}
func (c *counting) Snapshot(ctx context.Context) (model.Snapshot, error) {
	c.bump(&c.snap)
	return c.Fake.Snapshot(ctx)
}
func (c *counting) Processes(ctx context.Context) ([]model.Process, error) {
	c.bump(&c.procs)
	return c.Fake.Processes(ctx)
}
func (c *counting) Containers(ctx context.Context) ([]model.Container, error) {
	c.bump(&c.ctrs)
	return c.Fake.Containers(ctx)
}

func newCounting() *counting {
	return &counting{Fake: &source.Fake{InfoV: model.HostInfo{ProtocolVersion: model.ProtocolVersion, Hostname: "h"}}}
}

type views struct {
	mu   sync.Mutex
	last map[string]HostView
	all  map[string][]HostView
}

func newViews() *views { return &views{last: map[string]HostView{}, all: map[string][]HostView{}} }
func (v *views) publish(id string, hv HostView) {
	v.mu.Lock()
	v.last[id] = hv
	v.all[id] = append(v.all[id], hv)
	v.mu.Unlock()
}
func (v *views) get(id string) HostView { v.mu.Lock(); defer v.mu.Unlock(); return v.last[id] }
func (v *views) states(id string) []ConnState {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []ConnState
	for _, hv := range v.all[id] {
		out = append(out, hv.Conn.State)
	}
	return out
}

func start(t *testing.T, interval time.Duration, srcs map[string]source.Source) (*Hub, *fakeClock, *views) {
	t.Helper()
	clk := newFakeClock()
	v := newViews()
	h := newHub(srcs, interval, v.publish, clk)
	t.Cleanup(h.Stop)
	return h, clk, v
}

func TestTiering(t *testing.T) {
	a, b, c := newCounting(), newCounting(), newCounting()
	h, clk, _ := start(t, 2*time.Second, map[string]source.Source{"a": a, "b": b, "c": c})
	h.Select("a", "overview")
	h.SetVisible([]string{"a", "b"})
	clk.Advance(9 * time.Second)
	if _, n, _, _ := a.counts(); n != 5 {
		t.Errorf("selected polls = %d, want 5", n)
	}
	if _, n, _, _ := b.counts(); n != 1 {
		t.Errorf("visible polls after 9s = %d, want 1", n)
	}
	clk.Advance(7 * time.Second) // t=16s
	if _, n, _, _ := b.counts(); n != 2 {
		t.Errorf("visible polls after 16s = %d, want 2", n)
	}
	if info, n, _, _ := c.counts(); n != 1 || info != 1 {
		t.Errorf("hidden host: info=%d snap=%d, want first contact only", info, n)
	}
}

func TestPageData(t *testing.T) {
	a, b := newCounting(), newCounting()
	a.Procs = []model.Process{{PID: 5}}
	a.Ctrs = []model.Container{{ID: "x"}}
	h, clk, v := start(t, 2*time.Second, map[string]source.Source{"a": a, "b": b})
	h.SetVisible([]string{"a", "b"})
	h.Select("a", "overview")
	clk.Advance(time.Second)
	if _, _, p, c := a.counts(); p != 0 || c != 0 {
		t.Fatalf("overview fetched procs=%d ctrs=%d", p, c)
	}
	h.Select("a", "processes")
	clk.Advance(time.Second)
	if _, _, p, c := a.counts(); p != 1 || c != 0 {
		t.Fatalf("processes page: procs=%d ctrs=%d, want 1/0", p, c)
	}
	if got := v.get("a").Procs; len(got) != 1 || got[0].PID != 5 {
		t.Fatalf("view procs = %+v", got)
	}
	h.Select("a", "docker")
	clk.Advance(time.Second)
	if _, _, p, c := a.counts(); p != 1 || c != 1 {
		t.Fatalf("docker page: procs=%d ctrs=%d, want 1/1", p, c)
	}
	if got := v.get("a").Ctrs; len(got) != 1 {
		t.Fatalf("view ctrs = %+v", got)
	}
	if _, _, p, c := b.counts(); p != 0 || c != 0 {
		t.Fatalf("non-selected host fetched page data %d/%d", p, c)
	}
}

func TestOfflineBackoffAndAge(t *testing.T) {
	a := newCounting()
	a.Snaps = []model.Snapshot{{CPU: model.CPU{Total: 42}}}
	h, clk, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
	h.Select("a", "overview")
	clk.Advance(time.Second)
	if hv := v.get("a"); hv.Conn.State != Online || hv.Snap == nil || hv.Snap.CPU.Total != 42 {
		t.Fatalf("not online with data: %+v", hv.Conn)
	}
	a.Mu.Lock()
	a.Err = &model.APIError{Code: "unavailable"}
	a.Mu.Unlock()
	clk.Advance(6 * time.Second) // polls at 2, 4, 6 fail -> Offline
	hv := v.get("a")
	if hv.Conn.State != Offline {
		t.Fatalf("state = %v, want Offline", hv.Conn.State)
	}
	if hv.Snap == nil || hv.Snap.CPU.Total != 42 {
		t.Fatalf("last snapshot lost: %+v", hv.Snap)
	}
	if hv.Age < 5*time.Second {
		t.Fatalf("age = %v, want >= 5s", hv.Age)
	}
	_, before, _, _ := a.counts()
	clk.Advance(500 * time.Millisecond) // t=7.5s; Offline since 6s, backoff 2 s: nothing yet
	if _, n, _, _ := a.counts(); n != before {
		t.Fatalf("polled before backoff elapsed")
	}
	clk.Advance(time.Second) // t=8.5s
	if _, n, _, _ := a.counts(); n != before+1 {
		t.Fatalf("polls after backoff = %d, want %d", n-before, 1)
	}
	if age := v.get("a").Age; age < 8*time.Second {
		t.Fatalf("age did not grow: %v", age)
	}
	// recovery
	a.Mu.Lock()
	a.Err = nil
	a.Mu.Unlock()
	clk.Advance(10 * time.Second)
	if hv := v.get("a"); hv.Conn.State != Online || hv.Age > 2*time.Second {
		t.Fatalf("no recovery: %v age %v", hv.Conn.State, hv.Age)
	}
}

func TestTerminalStatesStopUntilRetry(t *testing.T) {
	a := newCounting()
	a.Err = &model.APIError{Code: "unauthorized"}
	h, clk, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
	h.Select("a", "overview")
	clk.Advance(10 * time.Second)
	if st := v.get("a").Conn.State; st != Unauthorized {
		t.Fatalf("state = %v", st)
	}
	info, n, _, _ := a.counts()
	if info != 1 || n != 0 {
		t.Fatalf("info=%d snap=%d, want 1/0", info, n)
	}
	clk.Advance(20 * time.Second)
	info2, _, _, _ := a.counts()
	if info2 != info {
		t.Fatalf("polled while Unauthorized")
	}
	a.Mu.Lock()
	a.Err = nil
	a.Mu.Unlock()
	h.Retry("a")
	clk.Advance(time.Second)
	if st := v.get("a").Conn.State; st != Online {
		t.Fatalf("after Retry state = %v", st)
	}
}

func TestHostKeyReachesView(t *testing.T) {
	a := newCounting()
	hk := &transport.HostKeyError{Host: "h:22", Fingerprint: "SHA256:abc"}
	a.Err = hk
	h, clk, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
	h.Select("a", "overview")
	clk.Advance(time.Second)
	hv := v.get("a")
	if hv.Conn.State != HostKeyUnknown || hv.HostKey != hk {
		t.Fatalf("state %v hostkey %v", hv.Conn.State, hv.HostKey)
	}
	a.Mu.Lock()
	a.Err = nil
	a.Mu.Unlock()
	h.Retry("a")
	clk.Advance(time.Second)
	if hv := v.get("a"); hv.Conn.State != Online || hv.HostKey != nil {
		t.Fatalf("after retry: %v hostkey %v", hv.Conn.State, hv.HostKey)
	}
}

func TestConnectingFetchesInfoAndPublishesHistory(t *testing.T) {
	a := newCounting()
	a.Snaps = []model.Snapshot{{CPU: model.CPU{Total: 1}}, {CPU: model.CPU{Total: 2}}}
	h, clk, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
	h.Select("a", "overview")
	clk.Advance(3 * time.Second)
	hv := v.get("a")
	if hv.Info.Hostname != "h" {
		t.Fatalf("info = %+v", hv.Info)
	}
	if info, _, _, _ := a.counts(); info != 1 {
		t.Fatalf("info calls = %d, want 1", info)
	}
	if pts := hv.Hist.Series("cpu"); len(pts) != 2 {
		t.Fatalf("history points = %d, want 2", len(pts))
	}
	if s := v.states("a"); s[0] != Connecting {
		t.Fatalf("first publish = %v, want Connecting", s[0])
	}
}

func TestIncompatibleInfo(t *testing.T) {
	a := newCounting()
	a.InfoV.ProtocolVersion = "9.0"
	h, clk, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
	h.Select("a", "overview")
	clk.Advance(5 * time.Second)
	if st := v.get("a").Conn.State; st != Incompatible {
		t.Fatalf("state = %v", st)
	}
	if _, n, _, _ := a.counts(); n != 0 {
		t.Fatalf("snapshot polled on incompatible agent")
	}
}

func TestStopEndsGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	srcs := map[string]source.Source{"a": newCounting(), "b": newCounting(), "c": newCounting()}
	clk := newFakeClock()
	v := newViews()
	h := newHub(srcs, 2*time.Second, v.publish, clk)
	h.Select("a", "overview")
	clk.Advance(3 * time.Second)
	h.Stop()
	h.Stop() // idempotent
	time.Sleep(10 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines before %d after Stop %d", before, after)
	}
	n := len(v.states("a"))
	clk.Advance(10 * time.Second)
	if len(v.states("a")) != n {
		t.Fatal("published after Stop")
	}
}
