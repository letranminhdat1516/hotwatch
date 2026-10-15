package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// CPU builds the CPU page: one bar per core, the load averages, the last
// five minutes of total usage and the busiest processes.
func CPU(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	known := !partial(s, "cpu")
	pageColumn(c, func() {
		section(c, "Cores", func() {
			load := format.Unknown
			if known {
				load = format.Load(s.CPU.Load)
			}
			note(c, "Load "+load)
			if !known || len(s.CPU.PerCore) == 0 {
				note(c, format.Unknown)
				return
			}
			ui.Grid(c).Columns(2).GapX(3 * g()).GapY(g()).Children(func() {
				for i, p := range s.CPU.PerCore {
					components.BarRow(c, fmt.Sprintf("Core %d", i+1), ptr(p), pct(p), metricTint(c, "cpu"))
				}
			})
		})
		components.DotTimeChart(c, "CPU", "%", []components.Series{{Name: "Total", Pts: points(v, "cpu")}}, now, metricTint(c, "cpu"))
		procSection(c, v, "Top processes by CPU", func() {
			top := topProcs(v.Procs, func(model.Process) bool { return true }, func(p model.Process) float64 { return p.CPUPct })
			table(c, []column{{title: "Name"}, {title: "PID", width: 8 * g(), end: true}, {title: "CPU", width: 10 * g(), end: true}},
				len(top), func(r, col int) {
					switch col {
					case 0:
						text(c, top[r].Name, false)
					case 1:
						text(c, pid(top[r]), true)
					case 2:
						text(c, pct(top[r].CPUPct), true)
					}
				})
		})
	})
}
