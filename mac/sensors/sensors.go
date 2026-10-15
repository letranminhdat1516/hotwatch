// Package sensors reads the Mac's temperature sensors: on Apple silicon the
// SoC's die sensors ("PMU tdie…") and the storage and battery gauges,
// through IOKit's HID event system, which needs no administrator rights.
package sensors

import "strings"

// Reading is one sensor and its temperature in °C.
type Reading struct {
	Name string
	C    float64
}

// CPU summarises the CPU die sensors: their mean and the hottest. ok is
// false when there are none (Intel Macs, other platforms). Readings outside
// 1–150 °C are sensor glitches and ignored.
func CPU(r []Reading) (avg, hottest float64, ok bool) {
	var sum float64
	n := 0
	for _, x := range r {
		if !strings.Contains(x.Name, "tdie") || x.C <= 0 || x.C > 150 {
			continue
		}
		sum += x.C
		hottest = max(hottest, x.C)
		n++
	}
	if n == 0 {
		return 0, 0, false
	}
	return sum / float64(n), hottest, true
}
