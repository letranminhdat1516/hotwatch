package main

import (
	"context"
	"os"
	"strings"

	"hotwatch/collect"
	"hotwatch/collect/linux"
	"hotwatch/model"
)

func newCollector() collect.Collector { return linux.New("/") }

// procLookup reads one process's start time straight from /proc, so it never
// races the sampler's collector.
func procLookup(_ context.Context, pid int32) (model.Process, bool, error) {
	st, ok, err := linux.ProcessStart("/", pid)
	return model.Process{PID: pid, StartTime: st}, ok, err
}

func kernelVersion() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
