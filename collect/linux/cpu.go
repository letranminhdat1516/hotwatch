package linux

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"

	"hotwatch/model"
)

// cpuCounters holds cumulative jiffies for the total line and each core.
type cpuCounters struct {
	total cpuLine
	cores []cpuLine
}

type cpuLine struct{ busy, user, system, total uint64 }

// sampleCPU fills cpu.Total/PerCore from the delta against the previous
// sample. It returns false when the values are not fresh (first sample,
// unreadable file, counters went backwards); then the previous values are
// kept, or zero if there are none.
func (c *collector) sampleCPU(cpu *model.CPU) bool {
	cur, err := readStat(c.proc("stat"))
	if err != nil {
		return c.carry(cpu)
	}
	prev := c.prevCPU
	c.prevCPU = &cur
	if prev == nil || len(prev.cores) != len(cur.cores) {
		return c.carry(cpu)
	}
	total, ok := pct(prev.total, cur.total)
	if !ok {
		return c.carry(cpu)
	}
	cores := make([]float64, len(cur.cores))
	for i := range cores {
		if cores[i], ok = pct(prev.cores[i], cur.cores[i]); !ok {
			return c.carry(cpu)
		}
	}
	user, system := split(prev.total, cur.total)
	cpu.Total, cpu.PerCore, cpu.User, cpu.System = total, cores, &user, &system
	c.lastCPU, c.haveLast = *cpu, true
	return true
}

func (c *collector) carry(cpu *model.CPU) bool {
	if c.haveLast {
		cpu.Total = c.lastCPU.Total
		cpu.PerCore = append([]float64(nil), c.lastCPU.PerCore...)
		cpu.User, cpu.System = c.lastCPU.User, c.lastCPU.System
	}
	return false
}

// split is the user and system shares of the time between two readings, in
// percent; a counter that went backwards counts as 0. Call it only after pct
// accepted the same pair, so the elapsed time is positive.
func split(prev, cur cpuLine) (user, system float64) {
	dt := float64(cur.total - prev.total)
	if cur.user >= prev.user {
		user = float64(cur.user-prev.user) / dt * 100
	}
	if cur.system >= prev.system {
		system = float64(cur.system-prev.system) / dt * 100
	}
	return user, system
}

// pct returns the busy percentage between two readings; ok is false when a
// counter decreased or no time passed.
func pct(prev, cur cpuLine) (float64, bool) {
	if cur.total <= prev.total || cur.busy < prev.busy {
		return 0, false
	}
	return float64(cur.busy-prev.busy) / float64(cur.total-prev.total) * 100, true
}

func readStat(path string) (cpuCounters, error) {
	f, err := os.Open(path)
	if err != nil {
		return cpuCounters{}, err
	}
	defer f.Close()
	var out cpuCounters
	var haveTotal bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		line, err := parseCPULine(fields[1:])
		if err != nil {
			return cpuCounters{}, err
		}
		if fields[0] == "cpu" {
			out.total, haveTotal = line, true
		} else {
			out.cores = append(out.cores, line)
		}
	}
	if err := sc.Err(); err != nil {
		return cpuCounters{}, err
	}
	if !haveTotal {
		return cpuCounters{}, errors.New("no cpu line in stat")
	}
	return out, nil
}

// parseCPULine reads user nice system idle iowait irq softirq steal ...;
// busy = total - (idle + iowait), user = user + nice (guest times are
// already inside them), system = system + irq + softirq.
func parseCPULine(f []string) (cpuLine, error) {
	if len(f) < 5 {
		return cpuLine{}, errors.New("short cpu line")
	}
	n := min(len(f), 8)
	var total uint64
	vals := make([]uint64, n)
	for i := 0; i < n; i++ {
		v, err := strconv.ParseUint(f[i], 10, 64)
		if err != nil {
			return cpuLine{}, err
		}
		vals[i] = v
		total += v
	}
	system := vals[2]
	if n > 5 {
		system += vals[5]
	}
	if n > 6 {
		system += vals[6]
	}
	return cpuLine{busy: total - vals[3] - vals[4], user: vals[0] + vals[1], system: system, total: total}, nil
}
