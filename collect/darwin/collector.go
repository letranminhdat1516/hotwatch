//go:build darwin

// Package darwin samples host metrics on macOS with gopsutil (no cgo) and
// sysctl. Sections fail independently: a failing section is named in
// Snapshot.Partial and the rest of the snapshot is still returned.
package darwin

import (
	"context"
	"time"

	"hotwatch/collect"
	"hotwatch/mac/sensors"
	"hotwatch/model"
)

// collector keeps the previous counters needed for deltas. It is not safe for
// concurrent Sample calls; callers serialize them.
type collector struct {
	now func() time.Time

	prevCPU  *cpuCounters
	lastCPU  model.CPU
	haveLast bool

	prevDisk       map[string]diskCounters
	prevDiskAt     time.Time
	apfs           map[string][]string // APFS container disk -> physical store disks
	apfsTried      map[string]bool     // containers for which a refresh was attempted
	apfsList       func(context.Context) ([]byte, error)
	apfsNames      map[string]string // APFS volume device (disk3s1) -> name
	apfsNamesTried bool
	hdiutil        func(context.Context) ([]byte, error) // nil: no disk image detection
	images         map[string]bool                       // whole disks of attached images
	imagesKey      string                                // the mounted devices images was read for
	prevNet        map[string]netCounters
	prevVM         vmStatistics64
	prevVMAt       time.Time
	prevNetAt      time.Time
	netKinds       map[string]netKind
	netKindsKey    string

	readSensors func() ([]sensors.Reading, error)
	sensors     []sensors.Reading // last temperature read, reused for sensorsEvery
	sensorsAt   time.Time

	prevProc map[procKey]procPrev
	statics  map[procKey]procStatic // static facts of the last scan's processes
	ports    portCache
	lsof     func(context.Context) ([]byte, error) // nil = runLsof
	nettop   func(context.Context) ([]byte, error) // nil: no per-process network
}

// sensorsEvery is how often temperatures are read: one read waits tens of
// milliseconds on the HID service, and temperatures move slowly.
const sensorsEvery = 5 * time.Second

// New returns a Collector for the local Mac. Sample must not be called
// concurrently.
func New() collect.Collector {
	return &collector{now: time.Now, readSensors: sensors.Read, apfsList: runDiskutilAPFS, hdiutil: runHdiutilInfo, nettop: runNettop}
}

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
	// Temperatures are optional: a Mac without readable die sensors (Intel)
	// leaves them nil and the section complete.
	if s.Time.Sub(c.sensorsAt) >= sensorsEvery || s.Time.Before(c.sensorsAt) {
		r, err := c.readSensors()
		if err != nil {
			r = nil
		}
		c.sensors, c.sensorsAt = r, s.Time
	}
	if r := c.sensors; r != nil {
		if avg, hot, ok := sensors.CPU(r); ok {
			s.CPU.TempC, s.CPU.TempMaxC = &avg, &hot
		}
		if t, ok := sensors.Storage(r); ok {
			s.StorageTempC = &t
		}
	}
	if mem, err := c.readMemory(ctx, s.Time); err != nil {
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
