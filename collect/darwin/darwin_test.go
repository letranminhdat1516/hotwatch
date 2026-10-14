//go:build darwin

package darwin

import (
	"context"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func pageSize() uint64 {
	n, _ := unix.SysctlUint32("hw.pagesize")
	return uint64(n)
}

// vmStatPages returns the page count on a vm_stat line such as "Pages free:   12345.".
func vmStatPages(out []byte, key string) uint64 {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s+(\d+)\.`)
	m := re.FindSubmatch(out)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseUint(string(m[1]), 10, 64)
	return v
}

func inactive(out []byte) uint64 { return vmStatPages(out, "Pages inactive") * pageSize() }

func diffPct(a, b uint64) float64 {
	if b == 0 {
		return 100
	}
	return math.Abs(float64(a)-float64(b)) / float64(b) * 100
}

func TestCoresMatchSysctl(t *testing.T) {
	c := New()
	c.Sample(context.Background())
	time.Sleep(500 * time.Millisecond)
	s, err := c.Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n, _ := unix.SysctlUint32("hw.ncpu")
	if len(s.CPU.PerCore) != int(n) {
		t.Fatalf("cores %d, sysctl %d", len(s.CPU.PerCore), n)
	}
	if s.CPU.Total < 0 || s.CPU.Total > 100 {
		t.Fatalf("total %v out of range", s.CPU.Total)
	}
}

func TestMemoryNearVMStat(t *testing.T) {
	s, err := New().Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("vm_stat").Output()
	free := vmStatPages(out, "Pages free") * pageSize()
	if diffPct(s.Memory.Total-s.Memory.Used, free+inactive(out)) > 5 { // same definition as Activity Monitor's "available"
		t.Fatalf("memory differs from vm_stat by > 5%%: total %d used %d free %d inactive %d",
			s.Memory.Total, s.Memory.Used, free, inactive(out))
	}
	m := s.Memory
	if m.Total == 0 || m.Wired == 0 || m.App == 0 {
		t.Fatalf("zero field: %+v", m)
	}
	switch m.Pressure {
	case "normal", "warn", "critical":
	default:
		t.Fatalf("pressure %q", m.Pressure)
	}
}

func TestDisksListRoot(t *testing.T) {
	s, _ := New().Sample(context.Background())
	var root bool
	for _, d := range s.Disks {
		if d.Mount == "/" {
			root = true
			if d.Total == 0 || d.Used == 0 || d.Used > d.Total {
				t.Fatalf("bad root volume: %+v", d)
			}
		}
		if d.FS == "devfs" || d.FS == "autofs" {
			t.Errorf("filtered fs listed: %+v", d)
		}
	}
	if !root {
		t.Fatalf("/ missing from %+v", s.Disks)
	}
}

func TestFirstSampleNilRatesSecondNon(t *testing.T) {
	c := New()
	s1, _ := c.Sample(context.Background())
	for _, i := range s1.Net {
		if i.RxBps != nil || i.TxBps != nil {
			t.Fatalf("first sample net rate set: %+v", i)
		}
	}
	for _, d := range s1.Disks {
		if d.ReadBps != nil || d.WriteBps != nil {
			t.Fatalf("first sample disk rate set: %+v", d)
		}
	}
	var partial bool
	for _, p := range s1.Partial {
		partial = partial || p == "cpu"
	}
	if !partial {
		t.Fatalf("first sample should mark cpu partial: %v", s1.Partial)
	}
	time.Sleep(300 * time.Millisecond)
	s2, _ := c.Sample(context.Background())
	if len(s2.Net) == 0 {
		t.Skip("no network interfaces")
	}
	for _, i := range s2.Net {
		if i.RxBps == nil || i.TxBps == nil {
			t.Fatalf("second sample net rate nil: %+v", i)
		}
		if i.Name == "lo0" {
			t.Fatal("lo0 listed")
		}
	}
	for _, p := range s2.Partial {
		if p == "cpu" {
			t.Fatalf("cpu still partial: %v", s2.Partial)
		}
	}
}

func TestLoadAndUptime(t *testing.T) {
	s, _ := New().Sample(context.Background())
	if s.Uptime <= 0 {
		t.Fatalf("uptime %v", s.Uptime)
	}
	if s.CPU.Load == [3]float64{} {
		t.Log("load average all zero (unusual but possible)")
	}
}

func TestBSDName(t *testing.T) {
	for in, want := range map[string]string{
		"/dev/disk3s1s1": "disk3", "/dev/disk3s5": "disk3", "/dev/disk10": "disk10",
		"/dev/disk4s2": "disk4", "disk1s1": "disk1", "//user@host/share": "", "map auto_home": "",
	} {
		if got := bsdName(in); got != want {
			t.Errorf("bsdName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeepVolume(t *testing.T) {
	for _, c := range []struct {
		mount, fs string
		keep      bool
	}{
		{"/", "apfs", true}, {"/System/Volumes/Data", "apfs", true},
		{"/System/Volumes/VM", "apfs", false}, {"/System/Volumes/Preboot", "apfs", false},
		{"/dev", "devfs", false}, {"/System/Volumes/Data/home", "autofs", false},
		{"/Volumes/Backup", "apfs", true}, {"/Volumes/com.apple.TimeMachine.localsnapshots/Backups.backupdb", "nullfs", false},
	} {
		if got := keepVolume(c.mount, c.fs); got != c.keep {
			t.Errorf("keepVolume(%q,%q) = %v", c.mount, c.fs, got)
		}
	}
}

func TestCPUPctCounterDecreaseKeepsPrevious(t *testing.T) {
	if _, ok := pct(cpuLine{busy: 10, total: 100}, cpuLine{busy: 5, total: 200}); ok {
		t.Fatal("decrease accepted")
	}
	if v, ok := pct(cpuLine{busy: 10, total: 100}, cpuLine{busy: 60, total: 200}); !ok || v != 50 {
		t.Fatalf("pct = %v %v", v, ok)
	}
}
