//go:build !linux

package main

import (
	"github.com/shirou/gopsutil/v4/host"

	"hotwatch/collect"
	"hotwatch/collect/portable"
)

func newCollector() collect.Collector { return portable.New() }

// procLookup reads one process's identity without the sampler's collector,
// in the form the collector reports it.
var procLookup = portable.Lookup

func kernelVersion() string {
	v, _ := host.KernelVersion()
	return v
}
