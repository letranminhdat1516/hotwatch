package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hotwatch/model"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), ch: make(chan time.Time, 1)}
	if d <= 0 {
		t.ch <- c.now
		return t.ch
	}
	c.timers = append(c.timers, t)
	return t.ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	keep := c.timers[:0]
	for _, t := range c.timers {
		if !t.at.After(c.now) {
			t.ch <- c.now
		} else {
			keep = append(keep, t)
		}
	}
	c.timers = keep
}

// countingCollector counts calls and detects overlapping Sample/Processes.
type countingCollector struct {
	samples  atomic.Int32
	procs    atomic.Int32
	inflight atomic.Int32
	overlap  atomic.Bool
	failing  atomic.Bool
	delay    time.Duration
}

func (c *countingCollector) enter() func() {
	if c.inflight.Add(1) != 1 {
		c.overlap.Store(true)
	}
	time.Sleep(c.delay)
	return func() { c.inflight.Add(-1) }
}

func (c *countingCollector) Sample(ctx context.Context) (model.Snapshot, error) {
	defer c.enter()()
	n := c.samples.Add(1)
	if c.failing.Load() {
		return model.Snapshot{}, errors.New("boom")
	}
	return model.Snapshot{Uptime: time.Duration(n) * time.Second}, nil
}

func (c *countingCollector) Processes(ctx context.Context) ([]model.Process, error) {
	defer c.enter()()
	c.procs.Add(1)
	return []model.Process{{PID: 7}}, nil
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newTestSampler(col *countingCollector, clk *fakeClock) *Sampler {
	return NewSampler(col, SamplerOptions{Interval: 2 * time.Second, Idle: 30 * time.Second, Clock: clk})
}

func TestSamplerStartsOnRequestAndStopsWhenIdle(t *testing.T) {
	col, clk := &countingCollector{}, newFakeClock()
	s := newTestSampler(col, clk)
	if s.Running() {
		t.Fatal("running before any request")
	}
	if _, err := s.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.Running() {
		t.Fatal("not running after request")
	}
	clk.Advance(29 * time.Second)
	time.Sleep(20 * time.Millisecond)
	if !s.Running() {
		t.Fatal("stopped before the 30 s idle window")
	}
	clk.Advance(2 * time.Second) // 31 s with no request
	eventually(t, "sampler to stop", func() bool { return !s.Running() })

	before := col.samples.Load()
	done := make(chan error, 1)
	go func() { _, err := s.Snapshot(context.Background()); done <- err }()
	// After a stop the first published sample comes one interval after the
	// baseline sample; keep the clock moving until the request is answered.
	for answered := false; !answered; {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			answered = true
		case <-time.After(5 * time.Millisecond):
			clk.Advance(time.Second)
		}
	}
	if !s.Running() {
		t.Fatal("a request must restart the sampler")
	}
	if col.samples.Load() <= before {
		t.Fatal("restart took no sample")
	}
}

func TestSamplerRequestExtendsIdleWindow(t *testing.T) {
	col, clk := &countingCollector{}, newFakeClock()
	s := newTestSampler(col, clk)
	s.Snapshot(context.Background())
	clk.Advance(20 * time.Second)
	s.Snapshot(context.Background()) // touch at t=20
	clk.Advance(20 * time.Second)    // t=40: 20 s since last request
	time.Sleep(30 * time.Millisecond)
	if !s.Running() {
		t.Fatal("stopped although a request arrived 20 s ago")
	}
	clk.Advance(11 * time.Second) // 31 s since last request
	eventually(t, "sampler to stop", func() bool { return !s.Running() })
}

func TestSamplerSamplesEveryInterval(t *testing.T) {
	col, clk := &countingCollector{}, newFakeClock()
	s := newTestSampler(col, clk)
	s.Snapshot(context.Background())
	n := col.samples.Load()
	clk.Advance(2 * time.Second)
	eventually(t, "second sample", func() bool { return col.samples.Load() > n })
	snap, err := s.Snapshot(context.Background())
	if err != nil || snap.Uptime < 2*time.Second {
		t.Fatalf("latest snapshot = %+v, %v", snap, err)
	}
}

func TestSamplerNeverRunsCollectorConcurrently(t *testing.T) {
	col, clk := &countingCollector{delay: time.Millisecond}, newFakeClock()
	s := newTestSampler(col, clk)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Processes(context.Background()); err != nil {
				t.Error(err)
			}
			s.Snapshot(context.Background())
		}()
		clk.Advance(time.Second) // 20 s total: stays inside the idle window
	}
	wg.Wait()
	if col.overlap.Load() {
		t.Fatal("collector called concurrently")
	}
	if col.procs.Load() != 20 {
		t.Fatalf("processes calls = %d, want 20", col.procs.Load())
	}
}

func TestSamplerProcessesRespectsCancel(t *testing.T) {
	col, clk := &countingCollector{}, newFakeClock()
	s := newTestSampler(col, clk)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Processes(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSamplerErrorSurfacesAndRecovers(t *testing.T) {
	col, clk := &countingCollector{}, newFakeClock()
	col.failing.Store(true)
	s := newTestSampler(col, clk)
	if _, err := s.Snapshot(context.Background()); err == nil {
		t.Fatal("want sampling error")
	}
	col.failing.Store(false)
	clk.Advance(2 * time.Second)
	eventually(t, "recovery", func() bool { _, err := s.Snapshot(context.Background()); return err == nil })
}

func TestSamplerMergesGPUAndMarksPartial(t *testing.T) {
	col, clk := &countingCollector{}, newFakeClock()
	var fail atomic.Bool
	s := NewSampler(col, SamplerOptions{Interval: 2 * time.Second, Clock: clk, GPU: func(context.Context) ([]model.GPU, error) {
		if fail.Load() {
			return nil, errors.New("smi broke")
		}
		return []model.GPU{{Name: "RTX"}}, nil
	}})
	snap, err := s.Snapshot(context.Background())
	if err != nil || len(snap.GPUs) != 1 || snap.GPUs[0].Name != "RTX" {
		t.Fatalf("snap = %+v, %v", snap, err)
	}
	fail.Store(true)
	clk.Advance(2 * time.Second)
	eventually(t, "partial gpu", func() bool {
		s2, _ := s.Snapshot(context.Background())
		return len(s2.GPUs) == 0 && len(s2.Partial) == 1 && s2.Partial[0] == "gpu"
	})
}

func TestSamplerFirstSnapshotWaitIsBounded(t *testing.T) {
	col, clk := &countingCollector{delay: 300 * time.Millisecond}, newFakeClock()
	s := NewSampler(col, SamplerOptions{Interval: 2 * time.Second, Clock: clk})
	done := make(chan error, 1)
	go func() { _, err := s.Snapshot(context.Background()); done <- err }()
	time.Sleep(50 * time.Millisecond)
	clk.Advance(3 * time.Second) // wait budget (interval + grace) elapses before the sample finishes
	var err error
	select {
	case err = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Snapshot did not return")
	}
	var ae *model.APIError
	if err != nil && !errors.As(err, &ae) {
		t.Fatalf("err = %v, want APIError or success", err)
	}
}
