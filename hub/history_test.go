package hub

import (
	"testing"
	"time"

	"hotwatch/model"
)

func fp(v float64) *float64 { return &v }

func TestHistoryRingKeepsHistoryLen(t *testing.T) {
	h := NewHistory()
	t0 := time.Unix(1000, 0)
	for i := 0; i < HistoryLen+100; i++ {
		h.Push(t0.Add(time.Duration(i)*time.Second), model.Snapshot{CPU: model.CPU{Total: float64(i)}})
	}
	pts := h.Series("cpu")
	if len(pts) != HistoryLen {
		t.Fatalf("len = %d, want %d", len(pts), HistoryLen)
	}
	if pts[0].V != 100 || pts[HistoryLen-1].V != float64(HistoryLen+99) {
		t.Fatalf("oldest/newest = %v/%v, want 100/%d", pts[0].V, pts[HistoryLen-1].V, HistoryLen+99)
	}
	if !pts[0].T.Equal(t0.Add(100 * time.Second)) {
		t.Fatalf("oldest time = %v", pts[0].T)
	}
}

func TestHistorySeriesValues(t *testing.T) {
	h := NewHistory()
	now := time.Unix(5, 0)
	h.Push(now, model.Snapshot{
		CPU:    model.CPU{Total: 12},
		Memory: model.Memory{Total: 200, Used: 50},
		Net:    []model.Interface{{Name: "en0", Up: true, RxBps: fp(10), TxBps: fp(1)}, {Name: "en1", Up: true, RxBps: fp(5)}, {Name: "en2", Up: true}},
		Disks:  []model.Volume{{ReadBps: fp(7), WriteBps: fp(3)}, {ReadBps: fp(1)}},
		GPUs:   []model.GPU{{UtilPct: fp(40)}, {UtilPct: fp(90)}},
	})
	want := map[string]float64{"cpu": 12, "mem": 25, "net.rx": 15, "net.tx": 1, "disk.read": 8, "disk.write": 3, "gpu": 40}
	for name, v := range want {
		p := h.Series(name)
		if len(p) != 1 || p[0].V != v || !p[0].T.Equal(now) {
			t.Errorf("%s = %+v, want one point %v", name, p, v)
		}
	}
}

func TestHistorySkipsUnknown(t *testing.T) {
	h := NewHistory()
	h.Push(time.Unix(1, 0), model.Snapshot{Net: []model.Interface{{Name: "en0", Up: true}}, Disks: []model.Volume{{}}, GPUs: []model.GPU{{}}})
	for _, n := range []string{"mem", "net.rx", "net.tx", "disk.read", "disk.write", "gpu"} {
		if p := h.Series(n); len(p) != 0 {
			t.Errorf("%s stored %v for unknown values", n, p)
		}
	}
	if len(h.Series("cpu")) != 1 {
		t.Error("cpu missing")
	}
	if h.Series("nope") != nil {
		t.Error("unknown series should be nil")
	}
}

func TestHistorySeriesIsCopy(t *testing.T) {
	h := NewHistory()
	h.Push(time.Unix(1, 0), model.Snapshot{CPU: model.CPU{Total: 1}})
	a := h.Series("cpu")
	a[0].V = 99
	if h.Series("cpu")[0].V != 1 {
		t.Fatal("Series returned internal storage")
	}
}

func TestHistoryNetSkipsLoopbackAndDownInterfaces(t *testing.T) {
	h := NewHistory()
	h.Push(time.Unix(1, 0), model.Snapshot{Net: []model.Interface{
		{Name: "lo", Up: true, RxBps: fp(1000), TxBps: fp(1000)},
		{Name: "lo0", Up: true, RxBps: fp(1000), TxBps: fp(1000)},
		{Name: "utun3", Up: false, RxBps: fp(500), TxBps: fp(500)},
		{Name: "en0", Up: true, RxBps: fp(10), TxBps: fp(2)},
	}})
	for name, v := range map[string]float64{"net.rx": 10, "net.tx": 2} {
		if p := h.Series(name); len(p) != 1 || p[0].V != v {
			t.Errorf("%s = %+v, want %v", name, p, v)
		}
	}
}

func TestHistoryTotalIntegratesRates(t *testing.T) {
	h := NewHistory()
	t0 := time.Unix(100, 0)
	rx := func(v float64) model.Snapshot {
		return model.Snapshot{Net: []model.Interface{{Name: "en0", Up: true, RxBps: fp(v)}}}
	}
	h.Push(t0, rx(10))
	if _, ok := h.Total("net.rx"); ok {
		t.Fatal("total known after one sample")
	}
	h.Push(t0.Add(2*time.Second), rx(10)) // 10 B/s for 2 s = 20
	h.Push(t0.Add(4*time.Second), rx(30)) // (10+30)/2 for 2 s = 40
	if got, ok := h.Total("net.rx"); !ok || got != 60 {
		t.Fatalf("total = %v %v, want 60", got, ok)
	}
	if _, ok := h.Total("net.tx"); ok {
		t.Fatal("tx was never sampled")
	}
}

func TestHistoryStatsOverWindow(t *testing.T) {
	h := NewHistory()
	t0 := time.Unix(100, 0)
	for i, v := range []float64{90, 10, 20, 60} {
		h.Push(t0.Add(time.Duration(i)*time.Second), model.Snapshot{CPU: model.CPU{Total: v}})
	}
	if avg, peak, ok := h.Stats("cpu", t0.Add(time.Second)); !ok || avg != 30 || peak != 60 {
		t.Fatalf("stats = %v/%v/%v, want 30/60 with the first point excluded", avg, peak, ok)
	}
	if _, _, ok := h.Stats("cpu", t0.Add(time.Minute)); ok {
		t.Fatal("stats with no points in the window")
	}
	if _, _, ok := h.Stats("nope", t0); ok {
		t.Fatal("unknown series")
	}
}

func TestHistoryDisksCountedOncePerDevice(t *testing.T) {
	h := NewHistory()
	h.Push(time.Unix(1, 0), model.Snapshot{Disks: []model.Volume{
		{Mount: "/", Device: "disk3", ReadBps: fp(5), WriteBps: fp(1)},
		{Mount: "/System/Volumes/Data", Device: "disk3", ReadBps: fp(5), WriteBps: fp(1)},
		{Mount: "/mnt/ext", Device: "/dev/sdb1", ReadBps: fp(2)},
	}})
	if p := h.Series("disk.read"); len(p) != 1 || p[0].V != 7 {
		t.Fatalf("disk.read = %+v, want 7 (container once + ext)", p)
	}
	if p := h.Series("disk.write"); len(p) != 1 || p[0].V != 1 {
		t.Fatalf("disk.write = %+v, want 1", p)
	}
}

func TestHistoryKeepsUserAndSystemCPU(t *testing.T) {
	h := NewHistory()
	t0 := time.Unix(1000, 0)
	u, s := 25.0, 7.0
	h.Push(t0, model.Snapshot{CPU: model.CPU{Total: 32, User: &u, System: &s}})
	h.Push(t0.Add(2*time.Second), model.Snapshot{CPU: model.CPU{Total: 40}}) // an agent without the split
	if got := h.Series("cpu.user"); len(got) != 1 || got[0].V != 25 {
		t.Fatalf("cpu.user %+v", got)
	}
	if got := h.Series("cpu.system"); len(got) != 1 || got[0].V != 7 {
		t.Fatalf("cpu.system %+v", got)
	}
}

func TestHistoryKeepsStackSeries(t *testing.T) {
	h := NewHistory()
	rx, tx, rd, wr := 300.0, 100.0, 20.0, 5.0
	h.Push(time.Unix(1000, 0), model.Snapshot{
		Memory: model.Memory{Total: 100, Used: 80, App: 40, Wired: 10, Compressed: 30},
		Net:    []model.Interface{{Name: "en0", Up: true, RxBps: &rx, TxBps: &tx}},
		Disks:  []model.Volume{{Mount: "/", Device: "d", ReadBps: &rd, WriteBps: &wr}},
	})
	for name, want := range map[string]float64{"mem.wired": 10, "mem.wiredcomp": 40, "mem": 80, "net.rxtx": 400, "disk.rw": 25} {
		if got := h.Series(name); len(got) != 1 || got[0].V != want {
			t.Errorf("%s = %+v, want %v", name, got, want)
		}
	}
}
