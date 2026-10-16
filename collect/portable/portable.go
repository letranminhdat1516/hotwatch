// Package portable samples host metrics through gopsutil on systems without
// a dedicated collector: Windows (desktop and Server), FreeBSD and others.
// Every system call goes through api, so the arithmetic is tested on any OS.
package portable

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"

	"hotwatch/collect"
	"hotwatch/model"
)

// api is the system as the collector reads it.
type api struct {
	now        func() time.Time
	cpuTimes   func(ctx context.Context) ([]cpu.TimesStat, error) // one per logical core
	load       func(ctx context.Context) ([3]float64, error)      // nil: no load average (Windows)
	vmem       func(ctx context.Context) (*mem.VirtualMemoryStat, error)
	swap       func(ctx context.Context) (*mem.SwapMemoryStat, error)
	uptime     func(ctx context.Context) (uint64, error) // seconds
	partitions func(ctx context.Context) ([]disk.PartitionStat, error)
	usage      func(ctx context.Context, mount string) (*disk.UsageStat, error)
	diskIO     func(ctx context.Context) (map[string]disk.IOCountersStat, error)
	netIO      func(ctx context.Context) ([]gnet.IOCountersStat, error)
	netIfs     func(ctx context.Context) (gnet.InterfaceStatList, error)
	// procs lists processes; statics holds the unchanging facts of those
	// seen before, so the reader can skip reading them again.
	procs     func(ctx context.Context, statics map[procKey]procStatic) ([]procInfo, error)
	listeners func(ctx context.Context) (map[int32][]model.Port, error)
}

type collector struct {
	api api

	prevCPU  *cpuCounters
	lastCPU  model.CPU
	haveLast bool

	disks diskState
	net   netState
	procs procState
}

// New returns the collector for the running system.
func New() collect.Collector { return newWith(systemAPI()) }

func newWith(a api) *collector { return &collector{api: a} }

func (c *collector) Sample(ctx context.Context) (model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return model.Snapshot{}, err
	}
	s := model.Snapshot{Time: c.api.now()}
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
	if c.api.load != nil {
		if l, err := c.api.load(ctx); err != nil {
			add("cpu")
		} else {
			s.CPU.Load = l
		}
	}
	if m, err := c.readMemory(ctx); err != nil {
		add("memory")
	} else {
		s.Memory = m
	}
	if up, err := c.api.uptime(ctx); err != nil {
		add("uptime")
	} else {
		s.Uptime = time.Duration(up) * time.Second
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

// readMemory maps the system's figures: "used" is what is not available,
// and all of it counts as app memory (the OS gives no finer split).
func (c *collector) readMemory(ctx context.Context) (model.Memory, error) {
	v, err := c.api.vmem(ctx)
	if err != nil {
		return model.Memory{}, err
	}
	used := v.Used
	if v.Available > 0 && v.Available <= v.Total {
		used = v.Total - v.Available
	}
	m := model.Memory{Total: v.Total, Used: used, App: used, Cached: v.Cached, Buffers: v.Buffers,
		Pressure: pressure(v.Total, v.Available)}
	if sw, err := c.api.swap(ctx); err == nil {
		m.SwapTotal, m.SwapUsed = sw.Total, sw.Used
	}
	return m, nil
}

// pressure grades how much memory is left: under 10 % available is
// critical, under 20 % a warning; "" when the total is unknown.
func pressure(total, avail uint64) string {
	if total == 0 {
		return ""
	}
	switch pct := float64(avail) / float64(total) * 100; {
	case pct < 10:
		return "critical"
	case pct < 20:
		return "warn"
	}
	return "normal"
}
