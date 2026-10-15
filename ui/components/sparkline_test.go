package components

import (
	"math"
	"testing"
	"time"

	"hotwatch/hub"
)

func TestDotColumnsBucketsAverageAndScale(t *testing.T) {
	from := time.Unix(0, 0)
	to := from.Add(40 * time.Second) // four 10 s buckets
	pts := []hub.Point{
		{T: from.Add(1 * time.Second), V: 100},        // bucket 0 with the next: avg 50 -> 4 of 8
		{T: from.Add(9 * time.Second), V: 0},          //
		{T: from.Add(15 * time.Second), V: 25},        // bucket 1: 25 -> 2
		{T: from.Add(35 * time.Second), V: math.NaN()}, // unknown: bucket 3 stays off
		{T: from.Add(50 * time.Second), V: 100},       // after the window: ignored
	}
	got := dotColumns(pts, from, to, 4, 8, 100)
	want := []int{4, 2, 0, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("columns = %v, want %v", got, want)
		}
	}
}

func TestDotColumnsAutoScaleAndFloor(t *testing.T) {
	from := time.Unix(0, 0)
	to := from.Add(20 * time.Second)
	pts := []hub.Point{{T: from.Add(2 * time.Second), V: 200}, {T: from.Add(12 * time.Second), V: 1}}
	got := dotColumns(pts, from, to, 2, 8, 0)
	// top = 200 * 1.08: 200 -> round(8/1.08) = 7; 1 rounds to 0 but a
	// non-zero bucket always shows one dot.
	if got[0] != 7 || got[1] != 1 {
		t.Fatalf("columns = %v, want [7 1]", got)
	}
}

func TestDotColumnsEmptyWindow(t *testing.T) {
	from := time.Unix(0, 0)
	got := dotColumns(nil, from, from.Add(time.Minute), 3, 8, 0)
	if len(got) != 3 || got[0]+got[1]+got[2] != 0 {
		t.Fatalf("columns = %v, want all off", got)
	}
}
