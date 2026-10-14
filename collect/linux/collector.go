// Package linux samples host metrics from /proc. It only parses files under a
// root directory, so it builds and tests on any OS.
package linux

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"hotwatch/collect"
	"hotwatch/model"
)

type collector struct {
	root string
	now  func() time.Time

	prevCPU  *cpuCounters // baseline for the next delta
	lastCPU  model.CPU    // last good CPU percentages (Load excluded)
	haveLast bool

	// Injected so tests can run on any OS against fixtures.
	statfs   statfsFunc
	ifaddrs  func(name string) []string // nil: addresses left empty
	pagesize int

	prevDisk   map[string]diskCounters
	prevDiskAt time.Time
	prevNet    map[string]netCounters
	prevNetAt  time.Time
	procs      procState
}

// New returns a Collector reading /proc under root ("/" in production).
func New(root string) collect.Collector {
	return &collector{root: root, now: time.Now,
		statfs: realStatfs, ifaddrs: realIfaddrs, pagesize: os.Getpagesize()}
}

func (c *collector) proc(name string) string { return filepath.Join(c.root, "proc", name) }

func (c *collector) Sample(ctx context.Context) (model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	s := model.Snapshot{Time: c.now()}
	var partial []string
	add := func(p string) {
		for _, q := range partial {
			if q == p {
				return
			}
		}
		partial = append(partial, p)
	}

	if !c.sampleCPU(&s.CPU) {
		add("cpu")
	}
	if load, err := readLoad(c.proc("loadavg")); err != nil {
		add("cpu")
	} else {
		s.CPU.Load = load
	}
	if mem, err := readMemory(c.proc("meminfo"), c.proc("pressure/memory")); err != nil {
		add("memory")
	} else {
		s.Memory = mem
	}
	if up, err := readUptime(c.proc("uptime")); err != nil {
		add("uptime")
	} else {
		s.Uptime = up
	}
	if !c.sampleDisks(s.Time, &s.Disks) {
		add("disks")
	}
	if !c.sampleNet(s.Time, &s.Net) {
		add("net")
	}
	s.Partial = partial
	return s, nil
}
