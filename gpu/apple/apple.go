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
	keyMemAlloc  = "Alloc system memory"
	keyRenderer  = "Renderer Utilization %"
	keyTiler     = "Tiler Utilization %"
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
	if m, ok := cf.Int(stats, keyMemAlloc); ok && m >= 0 {
		ma := uint64(m)
		g.MemAlloc = &ma
	}
	g.RendererPct = pctKey(stats, keyRenderer)
	g.TilerPct = pctKey(stats, keyTiler)
	return g
}

// pctKey reads a 0–100 value; nil when absent or out of range.
func pctKey(stats map[string]any, key string) *float64 {
	if u, ok := cf.Float(stats, key); ok && u >= 0 && u <= 100 {
		return &u
	}
	return nil
}
