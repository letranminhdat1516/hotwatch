//go:build darwin

package darwin

import (
	"context"

	"github.com/shirou/gopsutil/v4/mem"
	"golang.org/x/sys/unix"

	"hotwatch/model"
)

// readMemory fills the memory section. Total/Used/Wired/swap come from
// gopsutil (Used = Total - free - inactive, Activity Monitor's "available");
// App and Compressed come from sysctl. A failing optional sysctl leaves its
// field zero rather than failing the section.
func readMemory(ctx context.Context) (model.Memory, error) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return model.Memory{}, err
	}
	m := model.Memory{Total: vm.Total, Used: vm.Used, Wired: vm.Wired, Cached: vm.Inactive, Pressure: pressure()}
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		m.SwapTotal, m.SwapUsed = sw.Total, sw.Used
	}
	page, err := unix.SysctlUint32("hw.pagesize")
	if err == nil {
		internal, e1 := unix.SysctlUint32("vm.page_pageable_internal_count")
		purgeable, e2 := unix.SysctlUint32("vm.page_purgeable_count")
		if e1 == nil && e2 == nil && internal >= purgeable {
			m.App = uint64(internal-purgeable) * uint64(page)
		}
	}
	if b, err := unix.SysctlUint64("vm.compressor_bytes_used"); err == nil {
		m.Compressed = b
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
