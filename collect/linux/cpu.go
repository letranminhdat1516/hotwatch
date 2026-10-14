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

type cpuLine struct{ busy, total uint64 }

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
	cpu.Total, cpu.PerCore = total, cores
	c.lastCPU, c.haveLast = *cpu, true
	return true
}

func (c *collector) carry(cpu *model.CPU) bool {
	if c.haveLast {
		cpu.Total = c.lastCPU.Total
		cpu.PerCore = append([]float64(nil), c.lastCPU.PerCore...)
	}
	return false
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
// busy = total - (idle + iowait). Guest times are already inside user/nice.
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
	return cpuLine{busy: total - vals[3] - vals[4], total: total}, nil
}
