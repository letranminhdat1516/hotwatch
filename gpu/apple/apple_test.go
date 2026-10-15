//go:build darwin

package apple

import (
	"context"
	"testing"
)

func TestReadAppleGPU(t *testing.T) {
	gpus, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(gpus) == 0 {
		t.Fatal("no GPU found on this Mac")
	}
	g := gpus[0]
	if g.Name == "" {
		t.Error("empty GPU name")
	}
	if g.UtilPct == nil {
		t.Fatal("UtilPct is nil")
	}
	if *g.UtilPct < 0 || *g.UtilPct > 100 {
		t.Errorf("UtilPct = %v, want 0..100", *g.UtilPct)
	}
	if g.MemUsed == nil || *g.MemUsed == 0 {
		t.Errorf("MemUsed = %v, want > 0", g.MemUsed)
	}
}

func TestReadCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx); err == nil {
		t.Fatal("want error for canceled context")
	}
}

func TestFromStats(t *testing.T) {
	g := fromStats("Apple M5", map[string]any{
		"Device Utilization %": int64(21),
		"In use system memory": int64(684408832),
	})
	if g.Name != "Apple M5" || g.UtilPct == nil || *g.UtilPct != 21 || g.MemUsed == nil || *g.MemUsed != 684408832 {
		t.Fatalf("got %+v", g)
	}
	if g.MemTotal != nil || g.TempC != nil {
		t.Fatalf("MemTotal/TempC must be unknown: %+v", g)
	}

	empty := fromStats("", nil)
	if empty.Name != "Apple GPU" || empty.UtilPct != nil || empty.MemUsed != nil {
		t.Fatalf("missing stats must be unknown: %+v", empty)
	}
	neg := fromStats("x", map[string]any{"Device Utilization %": int64(-1), "In use system memory": int64(-5)})
	if neg.UtilPct != nil || neg.MemUsed != nil {
		t.Fatalf("out-of-range values must be unknown: %+v", neg)
	}
}

func BenchmarkRead(b *testing.B) {
	ctx := context.Background()
	for b.Loop() {
		if _, err := Read(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFromStatsEngines(t *testing.T) {
	g := fromStats("Apple M5", map[string]any{"Renderer Utilization %": int64(46), "Tiler Utilization %": int64(14),
		"Alloc system memory": int64(2530000000), "Device Utilization %": int64(46)})
	if g.RendererPct == nil || *g.RendererPct != 46 || g.TilerPct == nil || *g.TilerPct != 14 || g.MemAlloc == nil || *g.MemAlloc != 2530000000 {
		t.Fatalf("got %+v", g)
	}
	if bad := fromStats("x", map[string]any{"Tiler Utilization %": int64(140)}); bad.TilerPct != nil || bad.RendererPct != nil {
		t.Fatalf("out-of-range engines must be unknown: %+v", bad)
	}
}
