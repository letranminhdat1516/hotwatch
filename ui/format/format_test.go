package format

import (
	"math"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func TestFormatBytes(t *testing.T) {
	for _, tc := range []struct {
		in   uint64
		want string
	}{
		{0, "0 B"}, {1023, "1023 B"}, {1024, "1.0 KB"}, {1536, "1.5 KB"},
		{1048575, "1.0 MB"}, {1048576, "1.0 MB"}, {1<<20 - 52, "1023.9 KB"}, {12345678901, "11.5 GB"}, {1 << 40, "1.0 TB"}, {1 << 50, "1.0 PB"},
	} {
		if got := Bytes(tc.in); got != tc.want {
			t.Errorf("Bytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatRate(t *testing.T) {
	for _, tc := range []struct {
		in   *float64
		want string
	}{
		{nil, "—"}, {f64(0), "0 B/s"}, {f64(1.2 * 1024 * 1024), "1.2 MB/s"}, {f64(2048), "2.0 KB/s"},
		{f64(-5), "—"}, {f64(1023.6), "1.0 KB/s"}, {f64(1023.4), "1023 B/s"},
		{f64(math.NaN()), "—"}, {f64(math.Inf(1)), "—"}, {f64(math.Inf(-1)), "—"}, {f64(1e30), "888178419700125.2 PB/s"},
	} {
		if got := Rate(tc.in); got != tc.want {
			t.Errorf("Rate = %q, want %q", got, tc.want)
		}
	}
}

func TestFormatPercent(t *testing.T) {
	for _, tc := range []struct {
		in   *float64
		want string
	}{
		{nil, "—"}, {f64(math.NaN()), "—"}, {f64(math.Inf(1)), "—"}, {f64(37.5), "37.5 %"}, {f64(0), "0.0 %"}, {f64(100), "100.0 %"},
	} {
		if got := Percent(tc.in); got != tc.want {
			t.Errorf("Percent = %q, want %q", got, tc.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{-5 * time.Minute, "—"}, {0, "0m"}, {5 * time.Minute, "5m"}, {3*time.Hour + 2*time.Minute, "3h 2m"},
		{50*time.Hour + 5*time.Minute, "2d 2h"},
	} {
		if got := Duration(tc.in); got != tc.want {
			t.Errorf("Duration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatLoad(t *testing.T) {
	if got := Load([3]float64{1, 0.5, 0.25}); got != "1.00 / 0.50 / 0.25" {
		t.Errorf("Load = %q", got)
	}
	if got := Load([3]float64{1, math.NaN(), 0}); got != "—" {
		t.Errorf("Load with NaN = %q", got)
	}
	if got := Load([3]float64{-1, 0, 0}); got != "—" {
		t.Errorf("Load negative = %q", got)
	}
}
