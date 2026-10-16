package portable

import (
	"strings"
	"testing"
	"time"
)

// The real Windows readings: two samples give CPU, the system drive is
// listed with a size, and the process list carries names and start times.
func TestSystemSampleOnWindows(t *testing.T) {
	c := New()
	c.Sample(bg)
	time.Sleep(300 * time.Millisecond)
	s, err := c.Sample(bg)
	if err != nil || len(s.CPU.PerCore) == 0 || s.Memory.Total == 0 || s.Memory.Pressure == "" {
		t.Fatalf("sample %+v err %v", s, err)
	}
	var sysDrive bool
	for _, d := range s.Disks {
		if strings.EqualFold(d.Mount, `C:\`) || strings.EqualFold(d.Mount, "C:") {
			sysDrive = d.Total > 0
		}
	}
	if !sysDrive {
		t.Fatalf("no C: drive in %+v", s.Disks)
	}
	ps, err := c.Processes(bg)
	if err != nil || len(ps) < 10 {
		t.Fatalf("%d processes, err %v", len(ps), err)
	}
	for _, p := range ps {
		if strings.EqualFold(p.Name, "System") && p.StartTime.IsZero() {
			t.Fatal("System must get the boot time as its start")
		}
	}
}
