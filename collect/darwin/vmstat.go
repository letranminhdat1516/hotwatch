//go:build darwin

package darwin

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// vmStatistics64 is the kernel's vm_statistics64 (mach/vm_statistics.h):
// the counters vm_stat prints, in pages.
type vmStatistics64 struct {
	FreeCount, ActiveCount, InactiveCount, WireCount              uint32
	ZeroFill, Reactivations, Pageins, Pageouts, Faults, CowFaults uint64
	Lookups, Hits, Purges                                         uint64
	PurgeableCount, SpeculativeCount                              uint32
	Decompressions, Compressions, Swapins, Swapouts               uint64
	CompressorPageCount, ThrottledCount                           uint32
	ExternalPageCount, InternalPageCount                          uint32
	TotalUncompressedPagesInCompressor                            uint64
}

const hostVMInfo64 = 4 // HOST_VM_INFO64

var vmFns = sync.OnceValues(func() (func() uint32, func(host uint32, flavor int32, info unsafe.Pointer, count *uint32) int32) {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, nil
	}
	var self func() uint32
	var stats func(host uint32, flavor int32, info unsafe.Pointer, count *uint32) int32
	purego.RegisterLibFunc(&self, lib, "mach_host_self")
	purego.RegisterLibFunc(&stats, lib, "host_statistics64")
	return self, stats
})

// hostVMStats reads host_statistics64(HOST_VM_INFO64).
func hostVMStats() (vmStatistics64, error) {
	self, stats := vmFns()
	if self == nil {
		return vmStatistics64{}, fmt.Errorf("host_statistics64: libSystem not loaded")
	}
	var v vmStatistics64
	count := uint32(unsafe.Sizeof(v) / 4) // HOST_VM_INFO64_COUNT, in integer_t
	if kr := stats(self(), hostVMInfo64, unsafe.Pointer(&v), &count); kr != 0 {
		return vmStatistics64{}, fmt.Errorf("host_statistics64: kern_return %d", kr)
	}
	return v, nil
}
