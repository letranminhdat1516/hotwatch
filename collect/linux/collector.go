// Package linux samples host metrics from /proc. It only parses files under a
// root directory, so it builds and tests on any OS.
package linux

import (
	"context"
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
}

// New returns a Collector reading /proc under root ("/" in production).
func New(root string) collect.Collector {
	return &collector{root: root, now: time.Now}
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
	s.Partial = partial
	return s, nil
}

// Processes is implemented by A2.
func (c *collector) Processes(ctx context.Context) ([]model.Process, error) {
	return []model.Process{}, ctx.Err()
}
