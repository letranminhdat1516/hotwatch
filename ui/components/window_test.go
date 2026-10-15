package components

import (
	"testing"
	"time"
)

func labels(window time.Duration) []string {
	var out []string
	for _, tk := range xTicksFor(window) {
		out = append(out, tk.Label)
	}
	return out
}

func TestXTicksForWindows(t *testing.T) {
	cases := map[time.Duration][]string{
		time.Minute:      {"−1m", "−45s", "−30s", "−15s", "now"},
		5 * time.Minute:  {"−5m", "−4m", "−3m", "−2m", "−1m", "now"},
		15 * time.Minute: {"−15m", "−12m", "−9m", "−6m", "−3m", "now"},
		time.Hour:        {"−1h", "−45m", "−30m", "−15m", "now"},
	}
	for w, want := range cases {
		got := labels(w)
		if len(got) != len(want) {
			t.Fatalf("%v: %v, want %v", w, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%v: %v, want %v", w, got, want)
			}
		}
	}
	if ticks := xTicksFor(time.Hour); ticks[0].At != 0 || ticks[len(ticks)-1].At != 1 {
		t.Fatalf("ticks span %v..%v, want 0..1", ticks[0].At, ticks[len(ticks)-1].At)
	}
}

func TestSetChartWindowIgnoresNonPositive(t *testing.T) {
	defer SetChartWindow(5 * time.Minute)
	SetChartWindow(15 * time.Minute)
	SetChartWindow(0)
	SetChartWindow(-time.Minute)
	if ChartWindow() != 15*time.Minute {
		t.Fatalf("window = %v, want 15m", ChartWindow())
	}
}
