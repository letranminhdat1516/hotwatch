//go:build linux || windows

package local

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

// New reads the real machine: info, two snapshots (CPU needs a delta) and
// the process list, which must include this test.
func TestNewReadsThisMachine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	l := New()
	info, err := l.Info(ctx)
	if err != nil || info.OS != runtime.GOOS || info.Caps.Apps || !info.Caps.Processes || info.Hostname == "" {
		t.Fatalf("info %+v err %v", info, err)
	}
	if _, err := l.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	s, err := l.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.CPU.PerCore) != runtime.NumCPU() || s.Memory.Total == 0 || s.Uptime <= 0 || len(s.Disks) == 0 {
		t.Fatalf("snapshot cores=%d mem=%d uptime=%v disks=%d partial=%v",
			len(s.CPU.PerCore), s.Memory.Total, s.Uptime, len(s.Disks), s.Partial)
	}
	ps, err := l.Processes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if int(p.PID) == os.Getpid() {
			return
		}
	}
	t.Fatalf("own process missing from %d processes", len(ps))
}
