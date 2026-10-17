//go:build darwin

package darwin

import (
	"context"
	"testing"
	"time"

	"hotwatch/mac/sensors"
)

func TestSampleReadsSensorsEveryFewSeconds(t *testing.T) {
	c := New().(*collector)
	t0 := time.Unix(1000, 0)
	now := t0
	c.now = func() time.Time { return now }
	reads := 0
	c.readSensors = func() ([]sensors.Reading, error) {
		reads++
		return nil, nil
	}
	for i := range 11 { // 0 s … 10 s, one sample a second
		now = t0.Add(time.Duration(i) * time.Second)
		if _, err := c.Sample(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 3 { // at 0 s, 5 s and 10 s
		t.Fatalf("sensor reads in 10 s = %d, want 3", reads)
	}
}
