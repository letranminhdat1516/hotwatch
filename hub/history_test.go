package hub

import (
	"testing"
	"time"

	"hotwatch/model"
)

func fp(v float64) *float64 { return &v }

func TestHistoryRingKeeps600(t *testing.T) {
	h := NewHistory()
	t0 := time.Unix(1000, 0)
	for i := 0; i < 700; i++ {
		h.Push(t0.Add(time.Duration(i)*time.Second), model.Snapshot{CPU: model.CPU{Total: float64(i)}})
	}
	pts := h.Series("cpu")
	if len(pts) != 600 {
		t.Fatalf("len = %d, want 600", len(pts))
	}
	if pts[0].V != 100 || pts[599].V != 699 {
		t.Fatalf("oldest/newest = %v/%v, want 100/699", pts[0].V, pts[599].V)
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
		Net:    []model.Interface{{RxBps: fp(10), TxBps: fp(1)}, {RxBps: fp(5)}, {}},
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
	h.Push(time.Unix(1, 0), model.Snapshot{Net: []model.Interface{{}}, Disks: []model.Volume{{}}, GPUs: []model.GPU{{}}})
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
