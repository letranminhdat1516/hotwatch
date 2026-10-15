//go:build darwin

package darwin

import (
	"context"

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
func readMemory(ctx context.Context) (model.Memory, error) {
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
	return m, nil
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
