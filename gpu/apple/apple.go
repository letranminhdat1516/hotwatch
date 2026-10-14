//go:build darwin

// Package apple reads Apple GPU utilisation and memory in use from the IOKit
// IOAccelerator "PerformanceStatistics" dictionary (via purego, no cgo, no
// root, no subprocess).
package apple

import (
	"context"
	"strings"

	"hotwatch/internal/cf"
	"hotwatch/model"
)

const (
	class        = "IOAccelerator"
	keyStats     = "PerformanceStatistics"
	keyModel     = "model"
	keyUtil      = "Device Utilization %"
	keyMemInUse  = "In use system memory"
	fallbackName = "Apple GPU"
)

// Read returns one entry per IOAccelerator. Values the driver does not report
// are nil. MemTotal is nil: Apple GPUs share system memory and expose no
// dedicated total; TempC is nil: GPU temperature needs SMC access.
func Read(ctx context.Context) ([]model.GPU, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cf.Load(); err != nil {
		return nil, err
	}
	entries, err := cf.Services(class)
	if err != nil {
		return nil, err
	}
	gpus := make([]model.GPU, 0, len(entries))
	for _, e := range entries {
		stats, _ := cf.Property(e, keyStats).(map[string]any)
		gpus = append(gpus, fromStats(name(cf.Property(e, keyModel)), stats))
		cf.ReleaseObject(e)
	}
	return gpus, nil
}

func name(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return strings.TrimRight(string(s), "\x00")
	}
	return ""
}

// fromStats maps a PerformanceStatistics dictionary to a GPU. Missing or
// out-of-range values stay nil (unknown), never 0.
func fromStats(gpuName string, stats map[string]any) model.GPU {
	if gpuName == "" {
		gpuName = fallbackName
	}
	g := model.GPU{Name: gpuName}
	if u, ok := cf.Float(stats, keyUtil); ok && u >= 0 && u <= 100 {
		g.UtilPct = &u
	}
	if m, ok := cf.Int(stats, keyMemInUse); ok && m >= 0 {
		mu := uint64(m)
		g.MemUsed = &mu
	}
	return g
}
