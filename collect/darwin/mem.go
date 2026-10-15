//go:build darwin

package darwin

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
	"golang.org/x/sys/unix"

	"hotwatch/model"
)

// readMemory fills the memory section the way Activity Monitor counts it,
// from the kernel's own counters (host_statistics64, as vm_stat prints):
// App = anonymous − purgeable pages, Wired, Compressed = the pages the
// compressor occupies, Used ("in use") = App + Wired + Compressed, and
// Cached = file-backed + purgeable pages (memory macOS frees on demand).
// Total and swap come from sysctl through gopsutil.
func (c *collector) readMemory(ctx context.Context, at time.Time) (model.Memory, error) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return model.Memory{}, err
	}
	page, err := unix.SysctlUint32("hw.pagesize")
	if err != nil {
		return model.Memory{}, err
	}
	st, err := hostVMStats()
	if err != nil {
		return model.Memory{}, err
	}
	pg := func(n uint32) uint64 { return uint64(n) * uint64(page) }
	m := model.Memory{Total: vm.Total, Wired: pg(st.WireCount), Compressed: pg(st.CompressorPageCount),
		Cached: pg(st.ExternalPageCount) + pg(st.PurgeableCount), Pressure: pressure()}
	if st.InternalPageCount >= st.PurgeableCount {
		m.App = pg(st.InternalPageCount - st.PurgeableCount)
	}
	m.Used = min(m.App+m.Wired+m.Compressed, m.Total)
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		m.SwapTotal, m.SwapUsed = sw.Total, sw.Used
	}
	if lvl, err := unix.SysctlUint32("kern.memorystatus_level"); err == nil && lvl <= 100 {
		p := float64(100 - lvl)
		m.PressurePct = &p
	}
	if !c.prevVMAt.IsZero() {
		m.PageInsPS = pageRate(c.prevVM.Pageins, st.Pageins, at.Sub(c.prevVMAt))
		m.SwapOutsPS = pageRate(c.prevVM.Swapouts, st.Swapouts, at.Sub(c.prevVMAt))
	}
	c.prevVM, c.prevVMAt = st, at
	return m, nil
}

// pageRate is the per-second change of a page counter; nil when the
// interval is empty or the counter went back (a reset).
func pageRate(prev, cur uint64, dt time.Duration) *float64 {
	if dt <= 0 || cur < prev {
		return nil
	}
	r := float64(cur-prev) / dt.Seconds()
	return &r
}

// pressure maps kern.memorystatus_vm_pressure_level (1 normal, 2 warn, 4
// critical) to its name; an unreadable or unknown level is reported as
// "unknown" so it is not mistaken for a healthy host.
func pressure() string {
	n, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level")
	if err != nil {
		return "unknown"
	}
	return pressureName(n)
}

func pressureName(n uint32) string {
	switch n {
	case 1:
		return "normal"
	case 2:
		return "warn"
	case 4:
		return "critical"
	}
	return "unknown"
}
