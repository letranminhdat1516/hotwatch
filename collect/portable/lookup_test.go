package portable

import (
	"os"
	"testing"
)

// The signaller compares the start time a client saw (from Processes) with
// Lookup's; they must agree for a live process or every kill is refused.
func TestLookupAgreesWithProcesses(t *testing.T) {
	self := int32(os.Getpid())
	ps, err := New().Processes(bg)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, p := range ps {
		if p.PID != self {
			continue
		}
		seen = true
		got, ok, err := Lookup(bg, self)
		if err != nil || !ok {
			t.Fatalf("lookup: ok=%v err=%v", ok, err)
		}
		if !got.StartTime.Equal(p.StartTime) || got.Name != p.Name {
			t.Fatalf("lookup %v %q, processes %v %q", got.StartTime, got.Name, p.StartTime, p.Name)
		}
	}
	if !seen {
		t.Fatal("own process not listed")
	}
	if _, ok, err := Lookup(bg, 1<<30); ok || err != nil {
		t.Fatalf("missing pid: ok=%v err=%v", ok, err)
	}
}
