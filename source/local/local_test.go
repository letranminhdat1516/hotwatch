package local

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"hotwatch/collect"
	"hotwatch/model"
	"hotwatch/source"
)

var _ source.Source = (*Local)(nil)

func pf(v float64) *float64 { return &v }

type deps struct {
	col     *collect.Fake
	gpus    []model.GPU
	gpuErr  error
	bat     *model.Battery
	batErr  error
	killed  []string
	killErr error
	starts  map[int32]time.Time
}

func (d *deps) build() *Local {
	return NewWith(Deps{
		Collector: d.col,
		GPUs:      func(context.Context) ([]model.GPU, error) { return d.gpus, d.gpuErr },
		Battery:   func(context.Context) (*model.Battery, error) { return d.bat, d.batErr },
		Hostname:  func() (string, error) { return "mac", nil },
		Arch:      "arm64",
		OS:        "darwin",
		MacApps:   true,
		Kernel:    func() (string, error) { return "25.3.0", nil },
		StartTime: func(_ context.Context, pid int32) (time.Time, error) {
			if st, ok := d.starts[pid]; ok {
				return st, nil
			}
			return time.Time{}, ErrNoProcess
		},
		Kill: func(pid int32, sig model.Signal) error {
			d.killed = append(d.killed, string(sig))
			return d.killErr
		},
		Self: 4242,
	})
}

func newDeps() *deps {
	return &deps{col: &collect.Fake{Snap: model.Snapshot{CPU: model.CPU{Total: 7}}}}
}

func TestInfo(t *testing.T) {
	d := newDeps()
	d.gpus = []model.GPU{{Name: "g"}}
	d.bat = &model.Battery{ChargePct: 50}
	info, err := d.build().Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := model.Caps{GPU: true, Battery: true, Processes: true, Apps: true, Packages: true}
	if info.Caps != want || info.Hostname != "mac" || info.OS != "darwin" || info.Arch != "arm64" ||
		info.Kernel != "25.3.0" || info.CPUCount < 1 || !model.Compatible(info.ProtocolVersion) {
		t.Fatalf("info = %+v", info)
	}
	d2 := newDeps()
	info, _ = d2.build().Info(context.Background())
	if info.Caps.GPU || info.Caps.Battery || info.Caps.Docker {
		t.Fatalf("caps = %+v", info.Caps)
	}
}

func TestInfoOffMac(t *testing.T) {
	l := NewWith(Deps{Collector: &collect.Fake{}, OS: "windows", Arch: "amd64"})
	info, err := l.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.OS != "windows" || info.Caps.Apps || info.Caps.Packages || !info.Caps.Processes {
		t.Fatalf("info = %+v (no Mac apps or packages off macOS)", info)
	}
}

func TestSnapshotMerges(t *testing.T) {
	d := newDeps()
	d.gpus = []model.GPU{{Name: "g", UtilPct: pf(3)}}
	d.bat = &model.Battery{ChargePct: 80}
	s, err := d.build().Snapshot(context.Background())
	if err != nil || s.CPU.Total != 7 || len(s.GPUs) != 1 || s.Battery == nil || s.Battery.ChargePct != 80 || len(s.Partial) != 0 {
		t.Fatalf("snap %+v err %v", s, err)
	}
}

func TestSnapshotPartials(t *testing.T) {
	d := newDeps()
	d.gpuErr = errors.New("iokit")
	d.bat = &model.Battery{ChargePct: 80}
	d.batErr = errors.New("assertions failed")
	d.col.Snap.Partial = []string{"disks"}
	s, err := d.build().Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Battery == nil || s.Battery.ChargePct != 80 {
		t.Fatal("battery dropped despite non-nil result")
	}
	got := map[string]bool{}
	for _, p := range s.Partial {
		got[p] = true
	}
	if !got["gpu"] || !got["battery"] || !got["disks"] || len(s.Partial) != 3 {
		t.Fatalf("partial = %v", s.Partial)
	}
	// the collector's snapshot must not be mutated by the merge
	if len(d.col.Snap.Partial) != 1 {
		t.Fatalf("collector partial aliased: %v", d.col.Snap.Partial)
	}
}

func TestSnapshotCollectorError(t *testing.T) {
	d := newDeps()
	d.col.Err = errors.New("boom")
	if _, err := d.build().Snapshot(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

func TestDockerNotMonitored(t *testing.T) {
	l := newDeps().build()
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		var ae *model.APIError
		if !errors.As(err, &ae) || ae.Code != "not_found" || ae.Message != "Docker is not monitored on this machine" {
			t.Fatalf("err = %v", err)
		}
	}
	_, err := l.Containers(ctx)
	check(err)
	check(l.ContainerAction(ctx, "x", model.ActionStop))
	_, err = l.ContainerLogs(ctx, "x", 10, false)
	check(err)
}

func TestSignal(t *testing.T) {
	start := time.Unix(1700000000, 0)
	d := newDeps()
	d.starts = map[int32]time.Time{100: start, 1: start, 4242: start}
	d.col.Err = errors.New("Signal must not list all processes")
	l := d.build()
	ctx := context.Background()
	code := func(err error) string {
		var ae *model.APIError
		if errors.As(err, &ae) {
			return ae.Code
		}
		return ""
	}

	if err := l.Signal(ctx, 100, start, model.SigTerm); err != nil || len(d.killed) != 1 || d.killed[0] != "TERM" {
		t.Fatalf("err %v killed %v", err, d.killed)
	}
	if err := l.Signal(ctx, 100, start.Add(time.Second), model.SigKill); code(err) != "conflict" {
		t.Fatalf("mismatch: %v", err)
	}
	for _, pid := range []int32{1, 0, -5, 4242} {
		if err := l.Signal(ctx, pid, start, model.SigKill); code(err) == "" {
			t.Errorf("pid %d not refused: %v", pid, err)
		}
	}
	if err := l.Signal(ctx, 999, start, model.SigKill); code(err) != "not_found" {
		t.Fatalf("missing pid: %v", err)
	}
	if len(d.killed) != 1 {
		t.Fatalf("signalled despite refusal: %v", d.killed)
	}

	d.killErr = syscall.EPERM
	if err := l.Signal(ctx, 100, start, model.SigTerm); code(err) != "forbidden" {
		t.Fatalf("EPERM: %v", err)
	}
	d.killErr = syscall.ESRCH
	if err := l.Signal(ctx, 100, start, model.SigTerm); code(err) != "not_found" {
		t.Fatalf("ESRCH: %v", err)
	}
}

// overlapCollector fails the test if two calls run at once.
type overlapCollector struct {
	busy atomic.Int32
	bad  atomic.Bool
}

func (o *overlapCollector) enter() {
	if o.busy.Add(1) > 1 {
		o.bad.Store(true)
	}
	time.Sleep(time.Millisecond)
	o.busy.Add(-1)
}
func (o *overlapCollector) Sample(context.Context) (model.Snapshot, error) {
	o.enter()
	return model.Snapshot{}, nil
}
func (o *overlapCollector) Processes(context.Context) ([]model.Process, error) {
	o.enter()
	return nil, nil
}

func TestCallsAreSerialized(t *testing.T) {
	oc := &overlapCollector{}
	l := NewWith(Deps{Collector: oc, Hostname: func() (string, error) { return "m", nil },
		Kernel: func() (string, error) { return "k", nil }, Self: 1,
		StartTime: func(context.Context, int32) (time.Time, error) { oc.enter(); return time.Time{}, nil },
		Kill:      func(int32, model.Signal) error { return nil }})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = l.Snapshot(context.Background()) }()
		go func() { defer wg.Done(); _ = l.Signal(context.Background(), 50, time.Time{}, model.SigTerm) }()
	}
	wg.Wait()
	if oc.bad.Load() {
		t.Fatal("collector called concurrently")
	}
}
