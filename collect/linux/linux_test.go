package linux

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func newAt(root string, t0 time.Time) *collector {
	c := New(root).(*collector)
	c.now = func() time.Time { return t0 }
	c.statfs = fixtureStatfs(c)
	c.ifaddrs = nil
	c.pagesize = 4096
	return c
}

func TestCPUFromFixtures(t *testing.T) {
	c := newAt("testdata/t0", time.Unix(0, 0))
	if _, err := c.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.root, c.now = "testdata/t1", func() time.Time { return time.Unix(2, 0) }
	s, err := c.Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.CPU.PerCore) != 4 || math.Abs(s.CPU.Total-25.0) > 0.01 {
		t.Fatalf("cpu = %+v", s.CPU)
	}
	for _, p := range s.CPU.PerCore {
		if math.Abs(p-25) > 0.01 {
			t.Fatalf("core = %v", p)
		}
	}
	if s.Memory.Total != 8_083_000*1024 || s.Memory.Used != (8_083_000-3_120_000)*1024 {
		t.Fatalf("mem = %+v", s.Memory)
	}
	if len(s.Partial) != 0 {
		t.Fatalf("partial = %v", s.Partial)
	}
}

func TestFirstSampleCPUPartial(t *testing.T) {
	s, err := newAt("testdata/t0", time.Unix(0, 0)).Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.CPU.Total != 0 || !slices.Equal(s.Partial, []string{"cpu"}) {
		t.Fatalf("cpu=%+v partial=%v", s.CPU, s.Partial)
	}
}

func TestCounterDecreaseKeepsPrevious(t *testing.T) {
	c := newAt("testdata/t0", time.Unix(0, 0))
	c.Sample(context.Background())
	c.root, c.now = "testdata/t1", func() time.Time { return time.Unix(2, 0) }
	if _, err := c.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.root, c.now = "testdata/t2", func() time.Time { return time.Unix(4, 0) }
	s, err := c.Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(s.CPU.Total-25) > 0.01 || len(s.CPU.PerCore) != 4 || !slices.Contains(s.Partial, "cpu") {
		t.Fatalf("cpu=%+v partial=%v", s.CPU, s.Partial)
	}
}

func TestMemLoadUptimeFixtures(t *testing.T) {
	s, _ := newAt("testdata/t0", time.Unix(0, 0)).Sample(context.Background())
	m := s.Memory
	if m.Cached != 1_500_000*1024 || m.Buffers != 200_000*1024 || m.SwapTotal != 2_000_000*1024 || m.SwapUsed != 500_000*1024 || m.Pressure != "normal" {
		t.Fatalf("mem = %+v", m)
	}
	if s.CPU.Load != [3]float64{0.52, 0.48, 0.40} {
		t.Fatalf("load = %v", s.CPU.Load)
	}
	if s.Uptime != 3600500*time.Millisecond {
		t.Fatalf("uptime = %v", s.Uptime)
	}
}

func TestPressureLevels(t *testing.T) {
	for avg, want := range map[string]string{"9.99": "normal", "10.00": "warn", "29.9": "warn", "30.00": "critical"} {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, "proc/pressure"), 0o755)
		os.WriteFile(filepath.Join(root, "proc/pressure/memory"), []byte("some avg10="+avg+" avg60=0 avg300=0 total=1\n"), 0o644)
		os.WriteFile(filepath.Join(root, "proc/meminfo"), []byte("MemTotal: 100 kB\nMemAvailable: 50 kB\n"), 0o644)
		s, _ := newAt(root, time.Unix(0, 0)).Sample(context.Background())
		if s.Memory.Pressure != want {
			t.Errorf("avg10=%s: %q, want %q", avg, s.Memory.Pressure, want)
		}
	}
}

func TestMalformedMeminfo(t *testing.T) {
	s, err := newAt("testdata/bad", time.Unix(0, 0)).Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.Partial, "memory") || s.Memory.Total != 0 {
		t.Fatalf("mem=%+v partial=%v", s.Memory, s.Partial)
	}
	if s.Uptime == 0 {
		t.Fatal("other sections must survive")
	}
}

func TestMissingFiles(t *testing.T) {
	s, err := newAt(t.TempDir(), time.Unix(0, 0)).Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"cpu", "memory"} {
		if !slices.Contains(s.Partial, p) {
			t.Errorf("partial %v missing %s", s.Partial, p)
		}
	}
}

func TestProcessStart(t *testing.T) {
	st, ok, err := ProcessStart("testdata/a0", 4242)
	if err != nil || !ok || !st.Equal(time.Unix(1700000000+5000, 0)) {
		t.Fatalf("got %v %v %v", st, ok, err)
	}
	// Same formula as the collector, so values compare exactly.
	ps, err := New("testdata/a0").Processes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.PID == 4242 && !p.StartTime.Equal(st) {
			t.Fatalf("collector %v != ProcessStart %v", p.StartTime, st)
		}
	}
	if _, ok, err := ProcessStart("testdata/a0", 99999); ok || err != nil {
		t.Fatalf("missing pid: ok=%v err=%v", ok, err)
	}
}
