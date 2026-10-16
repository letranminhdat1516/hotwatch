//go:build darwin && arm64

package sensors

import (
	"testing"

	"hotwatch/internal/testenv"
)

func TestReadThisMacsDieSensors(t *testing.T) {
	testenv.RequireHardware(t)
	r, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	avg, hottest, ok := CPU(r)
	if !ok {
		t.Fatalf("no CPU die sensors among %d readings: %+v", len(r), r)
	}
	if avg < 15 || hottest > 115 || hottest < avg {
		t.Fatalf("implausible: avg %.1f hottest %.1f", avg, hottest)
	}
	t.Logf("%d sensors, CPU avg %.1f °C, hottest %.1f °C", len(r), avg, hottest)
}
