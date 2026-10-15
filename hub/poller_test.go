package hub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"hotwatch/model"
	"hotwatch/source"
	"hotwatch/transport"
)

// advance moves the bubble's clock forward by d and lets every goroutine
// settle. Callers avoid landing exactly on a poll instant.
func advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

type counting struct {
	*source.Fake
	mu                      sync.Mutex
	info, snap, procs, ctrs int
	procErr, ctrErr         error
	procGate                chan struct{} // when non-nil, Processes blocks until it is closed
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
	c.mu.Lock()
	err, gate := c.procErr, c.procGate
	c.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return c.Fake.Processes(ctx)
}
func (c *counting) Containers(ctx context.Context) ([]model.Container, error) {
	c.bump(&c.ctrs)
	c.mu.Lock()
	err := c.ctrErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
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

func start(t *testing.T, interval time.Duration, srcs map[string]source.Source) (*Hub, *views) {
	t.Helper()
	v := newViews()
	return New(srcs, interval, v.publish), v
}

func setErr(f *source.Fake, err error) {
	f.Mu.Lock()
	f.Err = err
	f.Mu.Unlock()
}

func TestTiering(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b, c := newCounting(), newCounting(), newCounting()
		h, _ := start(t, 2*time.Second, map[string]source.Source{"a": a, "b": b, "c": c})
		defer h.Stop()
		h.Select("a", "overview")
		h.SetVisible([]string{"a", "b"})
		advance(9 * time.Second)
		if _, n, _, _ := a.counts(); n != 5 {
			t.Errorf("selected polls = %d, want 5", n)
		}
		if _, n, _, _ := b.counts(); n != 1 {
			t.Errorf("visible polls after 9s = %d, want 1", n)
		}
		advance(7*time.Second + time.Second/2) // t=16.5s
		if _, n, _, _ := b.counts(); n != 2 {
			t.Errorf("visible polls after 16.5s = %d, want 2", n)
		}
		if info, n, _, _ := c.counts(); n != 1 || info != 1 {
			t.Errorf("hidden host: info=%d snap=%d, want first contact only", info, n)
		}
	})
}

func TestSetIntervalChangesSelectedPeriod(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		h, _ := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(9 * time.Second) // polls at 0, 2, 4, 6, 8
		if _, n, _, _ := a.counts(); n != 5 {
			t.Fatalf("polls at 2 s = %d, want 5", n)
		}
		h.SetInterval(time.Second)       // due at 9 (8 + 1 s), then every second
		advance(3500 * time.Millisecond) // t = 12.5: polls at 9, 10, 11, 12
		if _, n, _, _ := a.counts(); n != 9 {
			t.Fatalf("polls after SetInterval(1s) = %d, want 9", n)
		}
		h.SetInterval(0)     // ignored
		advance(time.Second) // t = 13.5: poll at 13
		if _, n, _, _ := a.counts(); n != 10 {
			t.Fatalf("polls after SetInterval(0) = %d, want 10", n)
		}
	})
}

func TestPageData(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := newCounting(), newCounting()
		a.Procs = []model.Process{{PID: 5}}
		a.Ctrs = []model.Container{{ID: "x"}}
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a, "b": b})
		defer h.Stop()
		h.SetVisible([]string{"a", "b"})
		h.Select("a", "overview")
		advance(time.Second)
		if _, _, p, c := a.counts(); p != 0 || c != 0 {
			t.Fatalf("overview fetched procs=%d ctrs=%d", p, c)
		}
		h.Select("a", "processes")
		advance(time.Second)
		if _, _, p, c := a.counts(); p != 1 || c != 0 {
			t.Fatalf("processes page: procs=%d ctrs=%d, want 1/0", p, c)
		}
		if hv := v.get("a"); len(hv.Procs) != 1 || hv.Procs[0].PID != 5 || hv.ProcsErr != nil {
			t.Fatalf("view procs = %+v err %v", hv.Procs, hv.ProcsErr)
		}
		h.Select("a", "docker")
		advance(time.Second)
		if _, _, p, c := a.counts(); p != 1 || c != 1 {
			t.Fatalf("docker page: procs=%d ctrs=%d, want 1/1", p, c)
		}
		if got := v.get("a").Ctrs; len(got) != 1 {
			t.Fatalf("view ctrs = %+v", got)
		}
		if _, _, p, c := b.counts(); p != 0 || c != 0 {
			t.Fatalf("non-selected host fetched page data %d/%d", p, c)
		}
	})
}

func TestSelectedHostRefreshesProcessesOffPage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := newCounting(), newCounting()
		a.InfoV.Caps.Processes, b.InfoV.Caps.Processes = true, true
		a.Procs = []model.Process{{PID: 5}}
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a, "b": b})
		defer h.Stop()
		h.SetVisible([]string{"a", "b"})
		h.Select("a", "overview")
		advance(time.Second) // the first poll fetches at once
		if _, _, p, _ := a.counts(); p != 1 {
			t.Fatalf("procs after first poll = %d, want 1", p)
		}
		if got := v.get("a").Procs; len(got) != 1 || got[0].PID != 5 {
			t.Fatalf("overview view lacks processes: %+v", got)
		}
		advance(8 * time.Second) // polls 2–5 (9 s): no fetch
		if _, _, p, _ := a.counts(); p != 1 {
			t.Fatalf("procs before %v = %d, want 1", procsOffPage, p)
		}
		advance(2 * time.Second) // poll 6, 10 s after the first fetch
		if _, _, p, _ := a.counts(); p != 2 {
			t.Fatalf("procs after %v = %d, want 2", procsOffPage, p)
		}
		if _, _, p, _ := b.counts(); p != 0 {
			t.Fatalf("visible host fetched processes %d times", p)
		}
	})
}

func TestPageErrorsReachView(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		a.Procs = []model.Process{{PID: 5}}
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "processes")
		advance(time.Second)
		a.mu.Lock()
		a.procErr = errors.New("ps failed")
		a.mu.Unlock()
		advance(2 * time.Second)
		hv := v.get("a")
		if hv.ProcsErr == nil || len(hv.Procs) != 1 {
			t.Fatalf("want stale list plus error: %+v / %v", hv.Procs, hv.ProcsErr)
		}
		if hv.Conn.State != Online {
			t.Fatalf("page error changed connection: %v", hv.Conn.State)
		}
		a.mu.Lock()
		a.procErr = nil
		a.mu.Unlock()
		advance(2 * time.Second)
		if hv := v.get("a"); hv.ProcsErr != nil {
			t.Fatalf("error not cleared: %v", hv.ProcsErr)
		}

		nf := &model.APIError{Code: "not_found", Message: "Docker is not monitored on this Mac"}
		a.mu.Lock()
		a.ctrErr = nf
		a.mu.Unlock()
		h.Select("a", "docker")
		advance(time.Second)
		hv = v.get("a")
		if hv.CtrsErr == nil || hv.CtrsErr.Error() != nf.Error() || hv.Conn.State != Online {
			t.Fatalf("ctrs err = %v state %v", hv.CtrsErr, hv.Conn.State)
		}
	})
}

func TestOfflineBackoffAndAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		a.Snaps = []model.Snapshot{{CPU: model.CPU{Total: 42}}}
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(time.Second)
		if hv := v.get("a"); hv.Conn.State != Online || hv.Snap == nil || hv.Snap.CPU.Total != 42 {
			t.Fatalf("not online with data: %+v", hv.Conn)
		}
		setErr(a.Fake, &model.APIError{Code: "unavailable"})
		advance(6 * time.Second) // t=7s; polls at 2, 4, 6 fail -> Offline, retry at 8s
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
		// While Offline each attempt starts with Info, which fails here.
		before, _, _, _ := a.counts()
		advance(500 * time.Millisecond) // t=7.5s: backoff not elapsed
		if n, _, _, _ := a.counts(); n != before {
			t.Fatalf("polled before backoff elapsed")
		}
		advance(time.Second) // t=8.5s
		if n, _, _, _ := a.counts(); n != before+1 {
			t.Fatalf("attempts after backoff = %d, want 1", n-before)
		}
		if age := v.get("a").Age; age < 8*time.Second {
			t.Fatalf("age did not grow: %v", age)
		}
		setErr(a.Fake, nil)
		advance(10 * time.Second)
		if hv := v.get("a"); hv.Conn.State != Online || hv.Age > 2*time.Second {
			t.Fatalf("no recovery: %v age %v", hv.Conn.State, hv.Age)
		}
	})
}

func TestRecoveryRefetchesInfo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(time.Second)
		setErr(a.Fake, errors.New("down"))
		advance(7 * time.Second)
		if st := v.get("a").Conn.State; st != Offline {
			t.Fatalf("state = %v", st)
		}
		a.Mu.Lock()
		a.Err = nil
		a.InfoV.ProtocolVersion = "9.0" // agent upgraded while we were away
		a.Mu.Unlock()
		_, before, _, _ := a.counts()
		advance(10 * time.Second)
		if st := v.get("a").Conn.State; st != Incompatible {
			t.Fatalf("state after recovery = %v, want Incompatible", st)
		}
		if _, n, _, _ := a.counts(); n != before {
			t.Fatalf("snapshot polled before compatibility was rechecked")
		}
	})
}

func TestTerminalStatesStopUntilRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		a.Err = &model.APIError{Code: "unauthorized"}
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(10 * time.Second)
		if st := v.get("a").Conn.State; st != Unauthorized {
			t.Fatalf("state = %v", st)
		}
		info, n, _, _ := a.counts()
		if info != 1 || n != 0 {
			t.Fatalf("info=%d snap=%d, want 1/0", info, n)
		}
		advance(20 * time.Second)
		if info2, _, _, _ := a.counts(); info2 != info {
			t.Fatalf("polled while Unauthorized")
		}
		setErr(a.Fake, nil)
		h.Retry("a")
		advance(time.Second)
		if st := v.get("a").Conn.State; st != Online {
			t.Fatalf("after Retry state = %v", st)
		}
	})
}

func TestHostKeyReachesView(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		hk := &transport.HostKeyError{Host: "h:22", Fingerprint: "SHA256:abc"}
		a.Err = hk
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(time.Second)
		hv := v.get("a")
		if hv.Conn.State != HostKeyUnknown || hv.HostKey != hk {
			t.Fatalf("state %v hostkey %v", hv.Conn.State, hv.HostKey)
		}
		setErr(a.Fake, nil)
		h.Retry("a")
		advance(time.Second)
		if hv := v.get("a"); hv.Conn.State != Online || hv.HostKey != nil {
			t.Fatalf("after retry: %v hostkey %v", hv.Conn.State, hv.HostKey)
		}
	})
}

func TestConnectingFetchesInfoAndPublishesHistory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		a.Snaps = []model.Snapshot{{CPU: model.CPU{Total: 1}}, {CPU: model.CPU{Total: 2}}}
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(3 * time.Second)
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
	})
}

func TestIncompatibleInfo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		a.InfoV.ProtocolVersion = "9.0"
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(5 * time.Second)
		hv := v.get("a")
		if hv.Conn.State != Incompatible || hv.Info.ProtocolVersion != "9.0" {
			t.Fatalf("state = %v info %+v", hv.Conn.State, hv.Info)
		}
		if _, n, _, _ := a.counts(); n != 0 {
			t.Fatalf("snapshot polled on incompatible agent")
		}
	})
}

func TestSnapshotPublishedBeforeSlowPageData(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		a.Snaps = []model.Snapshot{{CPU: model.CPU{Total: 9}}}
		a.Procs = []model.Process{{PID: 7}}
		a.procGate = make(chan struct{})
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "processes")
		advance(time.Second)
		hv := v.get("a")
		if hv.Snap == nil || hv.Snap.CPU.Total != 9 || hv.Conn.State != Online || hv.Procs != nil {
			t.Fatalf("snapshot not published ahead of processes: %+v", hv)
		}
		close(a.procGate)
		synctest.Wait()
		if hv := v.get("a"); len(hv.Procs) != 1 {
			t.Fatalf("processes not published after unblocking: %+v", hv.Procs)
		}
	})
}

func TestAddHostWhileRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h, v := start(t, 2*time.Second, map[string]source.Source{})
		defer h.Stop()
		n := newCounting()
		h.AddHost("n", n)
		h.Select("n", "overview")
		advance(5 * time.Second) // polls at 0, 2, 4
		if _, s, _, _ := n.counts(); s != 3 {
			t.Fatalf("polls = %d, want 3", s)
		}
		if v.get("n").Conn.State != Online {
			t.Fatalf("state = %v", v.get("n").Conn.State)
		}
		h.AddHost("n", newCounting()) // duplicate id is ignored
		advance(2 * time.Second)
		if _, s, _, _ := n.counts(); s != 4 {
			t.Fatalf("original host stopped polling: %d", s)
		}
	})
}

func TestRemoveHost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := newCounting(), newCounting()
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": a, "b": b})
		defer h.Stop()
		h.SetVisible([]string{"a", "b"})
		h.Select("a", "overview")
		advance(time.Second)
		h.RemoveHost("b")
		published := len(v.states("b"))
		_, bsnap, _, _ := b.counts()
		advance(40 * time.Second)
		if _, s, _, _ := b.counts(); s != bsnap {
			t.Fatalf("removed host still polled")
		}
		if len(v.states("b")) != published {
			t.Fatalf("removed host still publishing")
		}
		if _, s, _, _ := a.counts(); s < 20 {
			t.Fatalf("other host disturbed: %d polls", s)
		}
		h.RemoveHost("nope") // unknown id is a no-op
		h.Retry("b")
		h.Select("b", "overview")
	})
}

func TestReplaceSourceKeepsHistory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old, repl := newCounting(), newCounting()
		old.Snaps = []model.Snapshot{{CPU: model.CPU{Total: 1}}}
		repl.Snaps = []model.Snapshot{{CPU: model.CPU{Total: 2}}}
		repl.InfoV.Hostname = "new"
		h, v := start(t, 2*time.Second, map[string]source.Source{"a": old})
		defer h.Stop()
		h.Select("a", "overview")
		advance(3 * time.Second) // polls at 0 and 2
		h.ReplaceSource("a", repl)
		synctest.Wait()
		advance(time.Second)
		hv := v.get("a")
		if hv.Info.Hostname != "new" || hv.Conn.State != Online {
			t.Fatalf("not reconnected on new source: %+v / %v", hv.Info, hv.Conn.State)
		}
		if info, s, _, _ := repl.counts(); info != 1 || s != 1 {
			t.Fatalf("new source info=%d snap=%d", info, s)
		}
		pts := hv.Hist.Series("cpu")
		if len(pts) != 3 || pts[0].V != 1 || pts[2].V != 2 {
			t.Fatalf("history not kept: %+v", pts)
		}
		var sawConnecting bool
		for _, st := range v.states("a") {
			sawConnecting = sawConnecting || st == Connecting
		}
		if !sawConnecting {
			t.Fatal("no Connecting publish")
		}
		_, oldBefore, _, _ := old.counts()
		advance(10 * time.Second)
		if _, s, _, _ := old.counts(); s != oldBefore {
			t.Fatal("old source still polled")
		}
	})
}

func TestStopEndsGoroutines(t *testing.T) {
	// synctest.Test fails if any goroutine started in the bubble outlives it.
	synctest.Test(t, func(t *testing.T) {
		srcs := map[string]source.Source{"a": newCounting(), "b": newCounting(), "c": newCounting()}
		h, v := start(t, 2*time.Second, srcs)
		h.Select("a", "overview")
		advance(3 * time.Second)
		h.AddHost("d", newCounting())
		h.Stop()
		h.Stop() // idempotent
		h.AddHost("e", newCounting())
		n := len(v.states("a"))
		advance(10 * time.Second)
		if len(v.states("a")) != n || len(v.states("e")) != 0 {
			t.Fatal("published after Stop")
		}
	})
}

func TestOffPageProcessScansDoNotFollowAFastInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newCounting()
		a.InfoV.Caps.Processes = true
		h, _ := start(t, 500*time.Millisecond, map[string]source.Source{"a": a})
		defer h.Stop()
		h.Select("a", "overview")
		advance(9500 * time.Millisecond) // 20 polls at 0.5 s
		if _, _, p, _ := a.counts(); p != 1 {
			t.Fatalf("procs in 9.5 s at 0.5 s polls = %d, want 1", p)
		}
		h.Select("a", PageProcesses)
		advance(2 * time.Second)
		if _, _, p, _ := a.counts(); p < 4 {
			t.Fatalf("procs on the Processes page = %d, want every poll", p)
		}
	})
}
