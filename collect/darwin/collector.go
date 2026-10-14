//go:build darwin

// Package darwin samples host metrics on macOS with gopsutil (no cgo) and
// sysctl. Sections fail independently: a failing section is named in
// Snapshot.Partial and the rest of the snapshot is still returned.
package darwin

import (
	"context"
	"time"

	"hotwatch/collect"
	"hotwatch/model"
)

// collector keeps the previous counters needed for deltas. It is not safe for
// concurrent Sample calls; callers serialize them.
type collector struct {
	now func() time.Time

	prevCPU  *cpuCounters
	lastCPU  model.CPU
	haveLast bool

	prevDisk   map[string]diskCounters
	prevDiskAt time.Time
	prevNet    map[string]netCounters
	prevNetAt  time.Time
}

// New returns a Collector for the local Mac. Sample must not be called
// concurrently.
func New() collect.Collector { return &collector{now: time.Now} }

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

	if !c.sampleCPU(ctx, &s.CPU) {
		add("cpu")
	}
	if load, err := readLoad(ctx); err != nil {
		add("cpu")
	} else {
		s.CPU.Load = load
	}
	if mem, err := readMemory(ctx); err != nil {
		add("memory")
	} else {
		s.Memory = mem
	}
	if up, err := readUptime(ctx); err != nil {
		add("uptime")
	} else {
		s.Uptime = up
	}
	if !c.sampleDisks(ctx, s.Time, &s.Disks) {
		add("disks")
	}
	if !c.sampleNet(ctx, s.Time, &s.Net) {
		add("net")
	}
	s.Partial = partial
	return s, nil
}

// Processes is implemented by task B2.
func (c *collector) Processes(ctx context.Context) ([]model.Process, error) {
	return nil, nil
}
