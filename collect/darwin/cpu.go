//go:build darwin

package darwin

import (
	"context"
	"errors"

	"github.com/shirou/gopsutil/v4/cpu"

	"hotwatch/model"
)

type cpuLine struct{ busy, user, system, total float64 }

type cpuCounters struct {
	total cpuLine
	cores []cpuLine
}

func toLine(t cpu.TimesStat) cpuLine {
	// busy is summed from its parts rather than total-idle: the difference of
	// two large floats can jitter backwards on an idle core. user is time in
	// user code (nice and guest included), system the kernel's share.
	user := t.User + t.Nice + t.Guest + t.GuestNice
	system := t.System + t.Irq + t.Softirq + t.Steal
	busy := user + system
	return cpuLine{busy: busy, user: user, system: system, total: busy + t.Idle + t.Iowait}
}

// readCPU reads per-core ticks once and derives the aggregate from them, so
// total and cores always describe the same instant (gopsutil's aggregate
// reading is served from a coarse cache and can repeat between samples).
func readCPU(ctx context.Context) (cpuCounters, error) {
	per, err := cpu.TimesWithContext(ctx, true)
	if err != nil {
		return cpuCounters{}, err
	}
	if len(per) == 0 {
		return cpuCounters{}, errors.New("no cpu times")
	}
	out := cpuCounters{cores: make([]cpuLine, len(per))}
	for i, t := range per {
		l := toLine(t)
		out.cores[i] = l
		out.total.busy += l.busy
		out.total.user += l.user
		out.total.system += l.system
		out.total.total += l.total
	}
	return out, nil
}

// sampleCPU fills Total/PerCore from the delta against the previous sample.
// It returns false when the values are not fresh (first sample, read error,
// counters went backwards); then the previous values are kept if there are any.
func (c *collector) sampleCPU(ctx context.Context, out *model.CPU) bool {
	cur, err := readCPU(ctx)
	if err != nil {
		return c.carry(out)
	}
	prev := c.prevCPU
	c.prevCPU = &cur
	if prev == nil || len(prev.cores) != len(cur.cores) {
		return c.carry(out)
	}
	total, ok := pct(prev.total, cur.total)
	if !ok {
		return c.carry(out)
	}
	cores := make([]float64, len(cur.cores))
	for i := range cores {
		if cores[i], ok = pct(prev.cores[i], cur.cores[i]); !ok {
			return c.carry(out)
		}
	}
	user, system := split(prev.total, cur.total)
	out.Total, out.PerCore, out.User, out.System = total, cores, &user, &system
	c.lastCPU, c.haveLast = *out, true
	return true
}

func (c *collector) carry(out *model.CPU) bool {
	if c.haveLast {
		out.Total = c.lastCPU.Total
		out.PerCore = append([]float64(nil), c.lastCPU.PerCore...)
		out.User, out.System = c.lastCPU.User, c.lastCPU.System
	}
	return false
}

// split is the user and system shares of the time between two readings, in
// percent, clamped at 0 against counter jitter. Call it only after pct
// accepted the same pair, so the elapsed time is positive.
func split(prev, cur cpuLine) (user, system float64) {
	dt := cur.total - prev.total
	return max(0, (cur.user-prev.user)/dt*100), max(0, (cur.system-prev.system)/dt*100)
}

// pct returns the busy percentage between two readings; ok is false when a
// counter decreased or no time passed.
func pct(prev, cur cpuLine) (float64, bool) {
	if cur.total <= prev.total || cur.busy < prev.busy {
		return 0, false
	}
	return (cur.busy - prev.busy) / (cur.total - prev.total) * 100, true
}
