package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestRate(t *testing.T) {
	if r := Rate(100, 300, 2*time.Second); r == nil || *r != 100 {
		t.Fatalf("Rate = %v, want 100", r)
	}
	for name, r := range map[string]*float64{
		"reset":   Rate(500, 100, time.Second),
		"zero dt": Rate(1, 2, 0),
		"sleep":   Rate(1, 2, 10*time.Minute),
	} {
		if r != nil {
			t.Errorf("%s: Rate = %v, want nil", name, *r)
		}
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	rx := 1.5
	in := Snapshot{Time: time.Unix(1, 0).UTC(), CPU: CPU{Total: 12.5, PerCore: []float64{10, 15}, Load: [3]float64{1, 2, 3}},
		Net: []Interface{{Name: "eth0", RxBps: &rx}}, Battery: nil}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Snapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch:\n in %+v\nout %+v", in, out)
	}
}

func TestCompatible(t *testing.T) {
	if !Compatible("1.3") || Compatible("2.0") || Compatible("") {
		t.Fatal("Compatible wrong")
	}
}

func TestFullRoundTripAndTags(t *testing.T) {
	f, d := 3.5, 90*time.Minute
	u := uint64(7)
	in := Snapshot{
		Time:   time.Unix(2, 5).UTC(),
		Memory: Memory{Total: 10, Used: 5, SwapTotal: 2, Pressure: "normal"},
		Disks:  []Volume{{Mount: "/", FS: "ext4", Total: 9, Used: 4, ReadBps: &f}},
		GPUs:   []GPU{{Name: "g", UtilPct: &f, MemUsed: &u, MemTotal: &u, TempC: &f}},
		Battery: &Battery{ChargePct: 50, OnAC: true, TimeRemaining: &d, PowerW: &f,
			ThermalState: "nominal", Assertions: []Assertion{{PID: 3, Process: "p", Type: "t", Reason: "r"}}},
		Uptime:  time.Hour,
		Partial: []string{"gpu"},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Snapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("mismatch:\n in %+v\nout %+v", in, out)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"time", "cpu", "memory", "disks", "gpus", "battery", "uptime", "partial"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing lowerCamel key %q in %s", k, b)
		}
	}
	// nil optionals are omitted
	b, _ = json.Marshal(Interface{Name: "x"})
	if string(b) != `{"name":"x","up":false}` {
		t.Errorf("Interface JSON = %s", b)
	}
}

func TestProcessAndContainerRoundTrip(t *testing.T) {
	f := 1.0
	p := Process{PID: 1, PPID: 0, StartTime: time.Unix(3, 0).UTC(), Name: "n", CPUPct: 2, RSS: 3,
		DiskReadBps: &f, Ports: []Port{{Number: 80, Proto: "tcp"}}}
	b, _ := json.Marshal(p)
	var p2 Process
	if err := json.Unmarshal(b, &p2); err != nil || !reflect.DeepEqual(p, p2) {
		t.Fatalf("process: %v %s", err, b)
	}
	c := Container{ID: "a", Name: "b", CPUPct: &f, Ports: []string{"80/tcp"}, StartedAt: time.Unix(4, 0).UTC()}
	b, _ = json.Marshal(c)
	var c2 Container
	if err := json.Unmarshal(b, &c2); err != nil || !reflect.DeepEqual(c, c2) {
		t.Fatalf("container: %v %s", err, b)
	}
}

func TestAPIErrorIsError(t *testing.T) {
	var err error = APIError{Code: "conflict", Message: "pid reused"}
	if err.Error() != "conflict: pid reused" {
		t.Fatalf("Error() = %q", err.Error())
	}
	b, _ := json.Marshal(APIError{Code: "c", Message: "m"})
	if string(b) != `{"code":"c","message":"m"}` {
		t.Fatalf("json = %s", b)
	}
}

func TestConstants(t *testing.T) {
	if SigTerm != "TERM" || SigKill != "KILL" || ActionStart != "start" || ActionStop != "stop" || ActionRestart != "restart" {
		t.Fatal("constants")
	}
}

func TestDistinctVolumes(t *testing.T) {
	in := []Volume{
		{Mount: "/", Device: "disk3", Total: 494, Used: 480},
		{Mount: "/System/Volumes/Data", Device: "disk3", Total: 494, Used: 480},
		{Mount: "/mnt/ext", Device: "/dev/sdb1", Total: 1000, Used: 100},
		{Mount: "/net/share", Total: 50},  // no device: counts on its own
		{Mount: "/net/other", Total: 60},  // likewise, not merged with the one above
	}
	got := DistinctVolumes(in)
	if len(got) != 4 || got[0].Mount != "/" || got[1].Mount != "/mnt/ext" || got[2].Mount != "/net/share" || got[3].Mount != "/net/other" {
		t.Fatalf("DistinctVolumes = %+v", got)
	}
	var total uint64
	for _, v := range got {
		total += v.Total
	}
	if total != 494+1000+50+60 {
		t.Fatalf("capacity summed = %d, want container once", total)
	}
	if DistinctVolumes(nil) == nil || len(DistinctVolumes(nil)) != 0 {
		t.Fatal("nil input should give an empty, non-nil slice")
	}
	// Nothing to identify them by: never merged.
	if got := DistinctVolumes([]Volume{{Total: 1}, {Total: 2}}); len(got) != 2 {
		t.Fatalf("anonymous volumes merged: %+v", got)
	}
}
