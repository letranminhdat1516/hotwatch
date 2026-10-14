package linux

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

const bigN = 3000

var (
	bigOnce sync.Once
	bigRoot string
	tmpDirs []string
)

func TestMain(m *testing.M) {
	code := m.Run()
	for _, d := range tmpDirs {
		os.RemoveAll(d)
	}
	os.Exit(code)
}

// bigFixture generates a 3,000-process /proc once per test binary.
func bigFixture(tb testing.TB) string {
	bigOnce.Do(func() {
		root, err := os.MkdirTemp("", "hotwatch-big-")
		if err != nil {
			tb.Fatal(err)
		}
		tmpDirs = append(tmpDirs, root)
		bigRoot = root
		wr := func(p, s string) {
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				tb.Fatal(err)
			}
		}
		os.MkdirAll(filepath.Join(root, "proc/net"), 0o755)
		os.MkdirAll(filepath.Join(root, "etc"), 0o755)
		wr(filepath.Join(root, "proc/stat"), "cpu 1 0 1 1 0 0 0 0 0 0\nbtime 1700000000\n")
		wr(filepath.Join(root, "etc/passwd"), "root:x:0:0::/root:/bin/sh\nalice:x:1000:1000::/home/alice:/bin/sh\n")
		hdr := "  sl  local_address rem_address   st\n"
		wr(filepath.Join(root, "proc/net/tcp"), hdr+"   0: 00000000:1F90 00000000:0000 0A 0:0 00:0 0 1000 0 90001 1\n")
		for i := 1; i <= bigN; i++ {
			pid := 100 + i
			d := filepath.Join(root, fmt.Sprintf("proc/%d", pid))
			os.MkdirAll(filepath.Join(d, "fd"), 0o755)
			wr(d+"/stat", fmt.Sprintf("%d (worker-%d) S 1 %d %d 0 -1 4194560 100 0 0 0 %d %d 0 0 20 0 4 0 %d 1000000 5000 1\n", pid, i, pid, pid, i, i, 1000+i))
			wr(d+"/status", fmt.Sprintf("Name:\tworker-%d\nState:\tS (sleeping)\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\nVmRSS:\t   20000 kB\nThreads:\t4\nvoluntary_ctxt_switches:\t10\n", i))
			wr(d+"/statm", "50000 5000 800 10 0 3000 0\n")
			wr(d+"/cmdline", "/usr/bin/worker\x00--id\x00"+fmt.Sprint(i)+"\x00--config\x00/etc/worker/config.yaml\x00")
			wr(d+"/io", fmt.Sprintf("rchar: 1\nwchar: 1\nsyscr: 1\nsyscw: 1\nread_bytes: %d\nwrite_bytes: %d\ncancelled_write_bytes: 0\n", i*10, i*5))
			os.Symlink("/dev/null", d+"/fd/0")
			os.Symlink("socket:[12345]", d+"/fd/3")
			if i == 1 {
				os.Symlink("socket:[90001]", d+"/fd/4")
			}
		}
	})
	return bigRoot
}

func BenchmarkProcesses3000(b *testing.B) {
	c := newAt(bigFixture(b), time.Unix(100, 0))
	if _, err := c.Processes(ctx()); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps, err := c.Processes(ctx())
		if err != nil || len(ps) != bigN {
			b.Fatalf("%d %v", len(ps), err)
		}
	}
}

func TestProcesses3000Budget(t *testing.T) {
	if testing.Short() {
		t.Skip("budget test skipped with -short")
	}
	c := newAt(bigFixture(t), time.Unix(100, 0))
	if _, err := c.Processes(ctx()); err != nil { // warm-up and baseline
		t.Fatal(err)
	}
	var runs []time.Duration
	var last int
	for i := 0; i < 5; i++ {
		c.now = func() time.Time { return time.Unix(102+int64(i), 0) }
		start := time.Now()
		ps, err := c.Processes(ctx())
		runs = append(runs, time.Since(start))
		if err != nil || len(ps) != bigN {
			t.Fatalf("%d %v", len(ps), err)
		}
		b, _ := json.Marshal(ps)
		last = len(b)
		if i == 0 && (byPID(ps, 101).Ports[0].Number != 8080 || ptr(ps[0].DiskReadBps) != 0) {
			t.Fatalf("unexpected first process %+v", ps[0])
		}
	}
	slices.Sort(runs)
	t.Logf("median %v of %v; JSON %d bytes", runs[2], runs, last)
	if runs[2] >= 50*time.Millisecond {
		t.Errorf("median scan %v >= 50ms", runs[2])
	}
	if last >= 8<<20 {
		t.Errorf("JSON %d bytes >= 8 MB", last)
	}
}
