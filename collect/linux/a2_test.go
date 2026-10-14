package linux

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"hotwatch/model"
)

// fixtureStatfs serves capacity from <root>/statfs.json ({mount: {total,
// free}}); it exists only in tests, production uses realStatfs.
func fixtureStatfs(c *collector) statfsFunc {
	return func(mount string) (uint64, uint64, error) {
		b, err := os.ReadFile(filepath.Join(c.root, "statfs.json"))
		if err != nil {
			return 0, 0, err
		}
		var m map[string]struct{ Total, Free uint64 }
		if err := json.Unmarshal(b, &m); err != nil {
			return 0, 0, err
		}
		e, ok := m[mount]
		if !ok {
			return 0, 0, errors.New("no fixture for " + mount)
		}
		return e.Total, e.Free, nil
	}
}

func ctx() context.Context { return context.Background() }

func sampleTwice(t *testing.T) (model.Snapshot, model.Snapshot) {
	t.Helper()
	c := newAt("testdata/a0", time.Unix(100, 0))
	s0, err := c.Sample(ctx())
	if err != nil {
		t.Fatal(err)
	}
	c.root, c.now = "testdata/a1", func() time.Time { return time.Unix(102, 0) }
	s1, err := c.Sample(ctx())
	if err != nil {
		t.Fatal(err)
	}
	return s0, s1
}

func ptr(f *float64) float64 {
	if f == nil {
		return -1
	}
	return *f
}

func TestVolumesRealFilesystemsOnly(t *testing.T) {
	s0, s1 := sampleTwice(t)
	var mounts []string
	for _, v := range s1.Disks {
		mounts = append(mounts, v.Mount)
	}
	if !slices.Equal(mounts, []string{"/", "/data"}) {
		t.Fatalf("mounts = %v", mounts)
	}
	root := s1.Disks[0]
	if root.FS != "ext4" || root.Total != 100_000_000_000 || root.Used != 60_000_000_000 {
		t.Fatalf("root = %+v", root)
	}
	if s0.Disks[0].ReadBps != nil || s0.Disks[0].WriteBps != nil {
		t.Fatalf("first sample rates must be nil: %+v", s0.Disks[0])
	}
	if ptr(root.ReadBps) != 512_000 || ptr(root.WriteBps) != 1_024_000 {
		t.Fatalf("rates = %v %v", ptr(root.ReadBps), ptr(root.WriteBps))
	}
	// sdb1 did not change: rate is a real zero, not nil.
	if d := s1.Disks[1]; ptr(d.ReadBps) != 0 || ptr(d.WriteBps) != 0 {
		t.Fatalf("data = %+v", d)
	}
	if len(s1.Partial) > 0 && slices.Contains(s1.Partial, "disks") {
		t.Fatalf("partial = %v", s1.Partial)
	}
}

func TestNetworkRates(t *testing.T) {
	s0, s1 := sampleTwice(t)
	if len(s1.Net) != 2 || s1.Net[0].Name != "eth0" || s1.Net[1].Name != "wlan0" {
		t.Fatalf("net = %+v", s1.Net)
	}
	if s0.Net[0].RxBps != nil {
		t.Fatal("first sample rate must be nil")
	}
	e := s1.Net[0]
	if ptr(e.RxBps) != 1000 || ptr(e.TxBps) != 500 || !e.Up || len(e.Addrs) != 0 {
		t.Fatalf("eth0 = %+v", e)
	}
	if s1.Net[1].Up {
		t.Fatal("wlan0 operstate down")
	}
}

func TestNetworkCounterResetIsNil(t *testing.T) {
	c := newAt("testdata/a1", time.Unix(100, 0))
	c.Sample(ctx())
	c.root, c.now = "testdata/a0", func() time.Time { return time.Unix(102, 0) }
	s, _ := c.Sample(ctx())
	if s.Net[0].RxBps != nil || s.Disks[0].ReadBps != nil {
		t.Fatalf("reset must give nil: %+v %+v", s.Net[0], s.Disks[0])
	}
}

func TestMissingMountsAndNetDevArePartial(t *testing.T) {
	s, _ := newAt(t.TempDir(), time.Unix(0, 0)).Sample(ctx())
	if !slices.Contains(s.Partial, "disks") || !slices.Contains(s.Partial, "net") {
		t.Fatalf("partial = %v", s.Partial)
	}
}

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/mnt/my\040disk`); got != "/mnt/my disk" {
		t.Fatal(got)
	}
}

func procsTwice(t *testing.T) (first, second []model.Process) {
	t.Helper()
	c := newAt("testdata/a0", time.Unix(100, 0))
	first, err := c.Processes(ctx())
	if err != nil {
		t.Fatal(err)
	}
	c.root, c.now = "testdata/a1", func() time.Time { return time.Unix(102, 0) }
	second, err = c.Processes(ctx())
	if err != nil {
		t.Fatal(err)
	}
	return
}

func byPID(ps []model.Process, pid int32) *model.Process {
	for i := range ps {
		if ps[i].PID == pid {
			return &ps[i]
		}
	}
	return nil
}

func TestProcessesFields(t *testing.T) {
	first, second := procsTwice(t)
	if len(second) != 3 || second[0].PID != 1 || second[1].PID != 4242 || second[2].PID != 4300 {
		t.Fatalf("pids: %+v", second)
	}
	p := byPID(second, 4242)
	if p.Name != "python3" || p.User != "alice" || p.PPID != 1 || p.Threads != 3 || p.RSS != 2500*4096 {
		t.Fatalf("p = %+v", p)
	}
	if want := time.Unix(1_700_000_000+5000, 0); !p.StartTime.Equal(want) {
		t.Fatalf("start = %v want %v", p.StartTime, want)
	}
	if p.Command != "python3 -m http.server 8765" {
		t.Fatalf("command = %q", p.Command)
	}
	if !slices.Equal(p.Ports, []model.Port{{Number: 8080, Proto: "udp"}, {Number: 8765, Proto: "tcp"}}) {
		t.Fatalf("ports = %v", p.Ports)
	}
	if p.CPUPct != 25 {
		t.Fatalf("cpu = %v", p.CPUPct)
	}
	if ptr(p.DiskReadBps) != 2000 || ptr(p.DiskWriteBps) != 1000 {
		t.Fatalf("io = %v %v", ptr(p.DiskReadBps), ptr(p.DiskWriteBps))
	}
	// First sighting: CPU 0, disk rates nil.
	f := byPID(first, 4242)
	if f.CPUPct != 0 || f.DiskReadBps != nil || f.DiskWriteBps != nil {
		t.Fatalf("first = %+v", f)
	}
}

func TestProcessEdgeCases(t *testing.T) {
	_, second := procsTwice(t)
	w := byPID(second, 4300)
	if w.Name != "my (weird) app" || w.User != "4242" || w.Command != "" || w.DiskReadBps != nil || w.DiskWriteBps != nil {
		t.Fatalf("weird = %+v", w)
	}
	if !slices.Equal(w.Ports, []model.Port{{Number: 53, Proto: "tcp"}}) {
		t.Fatalf("ports = %v", w.Ports)
	}
	if init := byPID(second, 1); init.User != "root" || init.Command != "/sbin/init splash" || len(init.Ports) != 0 {
		t.Fatalf("init = %+v", init)
	}
}

func TestPIDReuseResetsBaseline(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, "testdata/a1", dir)
	c := newAt("testdata/a0", time.Unix(100, 0))
	c.Processes(ctx())
	// Same pid, different starttime: a different process, so no delta.
	st, _ := os.ReadFile(filepath.Join(dir, "proc/4242/stat"))
	os.WriteFile(filepath.Join(dir, "proc/4242/stat"), []byte(strings.Replace(string(st), " 500000 ", " 700000 ", 1)), 0o644)
	c.root, c.now = dir, func() time.Time { return time.Unix(102, 0) }
	ps, _ := c.Processes(ctx())
	if p := byPID(ps, 4242); p.CPUPct != 0 || p.DiskReadBps != nil {
		t.Fatalf("reused pid inherited baseline: %+v", p)
	}
}

func TestCmdlineCapAndUTF8(t *testing.T) {
	raw := []byte(strings.Repeat("a\x00", 3000)) // 6000 bytes
	if got := cmdlineString(raw[:maxCmdline+10]); len(got) != maxCmdline-1 {
		// 4096 bytes "a\0a\0..." -> trailing NUL trimmed
		t.Fatalf("len = %d", len(got))
	}
	multi := []byte(strings.Repeat("é", 3000)) // 2 bytes each
	got := cmdlineString(multi)
	if len(got) > maxCmdline || strings.ContainsRune(got, '�') {
		t.Fatalf("len=%d", len(got))
	}
}

func TestProcessesCancelledAndMissingRoot(t *testing.T) {
	c, cancel := context.WithCancel(ctx())
	cancel()
	if ps, err := newAt("testdata/a0", time.Unix(0, 0)).Processes(c); err == nil || ps == nil {
		t.Fatalf("%v %v", ps, err)
	}
	if ps, err := newAt(t.TempDir(), time.Unix(0, 0)).Processes(ctx()); err == nil || ps == nil {
		t.Fatalf("%v %v", ps, err)
	}
}

// The environment of other processes is never read (spec: Security).
func TestEnvironNeverOpened(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), `"environ"`) || strings.Contains(string(b), `/environ"`) {
			t.Errorf("%s references environ", f)
		}
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			return os.Symlink(l, out)
		case fi.IsDir():
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
