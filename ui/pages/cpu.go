package pages

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// navigate opens another page of the selected host ("Show All"). Pages are
// functions, so it is package state, set by the App; main thread only.
var navigate func(page string)

// SetNavigate installs how a page link opens another page.
func SetNavigate(f func(page string)) { navigate = f }

// CPU colours: user time in the CPU blue, kernel time in red; efficiency
// cores in a lighter blue than performance cores.
var (
	cpuSystemHex = "#ff453a"
	effCoreHex   = "#64d2ff"
)

// CPU builds the CPU page (UI-DESIGN.md "CPU"): the processor load with
// its user/system split over the chart window, the cores grouped by kind,
// the load averages and activity, and the busiest processes.
func CPU(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	pageColumn(c, func() {
		cpuLoadCard(c, v, now)
		ui.Grid(c).Columns(2).Gap(2 * g()).Children(func() {
			coresCard(c, v)
			activityCard(c, v, now)
		})
		topCPUCard(c, v)
	})
}

// cpuLoadCard is the headline, its split, temperature and window figures,
// and the stacked dot chart.
func cpuLoadCard(c *ui.Context, v hub.HostView, now time.Time) {
	t := c.Theme()
	s := v.Snap
	tint := metricTint(c, "cpu")
	sys := ui.Hex(cpuSystemHex)
	known := !partial(s, "cpu")
	from := now.Add(-components.ChartWindow())
	components.Surface(c, ui.Column(c).Gap(g())).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(g()).Children(func() {
			overviewSectionHeader(c, icons.CPU, "Processor Load", "cpu")
			ui.Spacer(c)
			if s.CPU.User != nil {
				legend(c, tint, "User")
				legend(c, sys, "System")
			}
		})
		ui.Row(c).Gap(5*g()).AlignItems(ui.End).Margin(g(), 0, 0, 0).Wrap().Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, "Now").FontSize(13).TextColor(t.TextMuted)
				val := format.Unknown
				if known {
					val = format.Percent(&s.CPU.Total)
				}
				ui.Text(c, val).FontSize(40).FontWeight(700).SingleLine()
			})
			if known && s.CPU.User != nil && s.CPU.System != nil {
				statCell(c, "User", pctShort(*s.CPU.User), "")
				statCell(c, "System", pctShort(*s.CPU.System), "")
			}
			if s.CPU.TempC != nil {
				sub := ""
				if s.CPU.TempMaxC != nil {
					sub = "hottest " + degrees(*s.CPU.TempMaxC)
				}
				statCell(c, "Temperature", degrees(*s.CPU.TempC), sub)
			}
			avg, peak := format.Unknown, format.Unknown
			if v.Hist != nil {
				if a, p, ok := v.Hist.Stats("cpu", from); ok {
					avg, peak = pctShort(a), pctShort(p)
				}
			}
			statCell(c, "Average", avg, "")
			statCell(c, "Peak", peak, "")
		})
		total, system := points(v, "cpu"), points(v, "cpu.system")
		layers := []components.DotLayer{{Pts: total, Color: tint}}
		if len(system) > 0 {
			layers = []components.DotLayer{{Pts: system, Color: sys}, {Pts: total, Color: tint}}
		}
		components.StackedDotChart(c, layers, from, now, 100, tint, 200).Margin(g(), 0, 0, 0)
	})
}

// legend is a coloured dot and a label.
func legend(c *ui.Context, col ui.Color, label string) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(col)
		ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
	})
}

// coresCard lists every logical processor, in performance and efficiency
// groups when the host reports the split.
func coresCard(c *ui.Context, v hub.HostView) {
	s, m := v.Snap, v.Info.Machine
	components.Surface(c, ui.Column(c).Gap(g())).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			overviewSectionHeader(c, icons.CPU, "Cores", "cpu")
			ui.Spacer(c)
			if n := len(s.CPU.PerCore); n > 0 && !partial(s, "cpu") {
				ui.Text(c, plural(n, "logical processor")).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
			}
		})
		if partial(s, "cpu") || len(s.CPU.PerCore) == 0 {
			note(c, format.Unknown)
			return
		}
		perCore := s.CPU.PerCore
		// Apple silicon numbers its efficiency cores first (cpu0…), then
		// its performance cores.
		if m.PerfCores > 0 && m.EffCores > 0 && m.PerfCores+m.EffCores == len(perCore) {
			coreGroup(c, "Performance Cores", perCore, m.EffCores, len(perCore), metricTint(c, "cpu"))
			coreGroup(c, "Efficiency Cores", perCore, 0, m.EffCores, ui.Hex(effCoreHex))
			return
		}
		coreGroup(c, "", perCore, 0, len(perCore), metricTint(c, "cpu"))
	})
}

// coreGroup draws cores [from, to) in two columns under an optional title.
func coreGroup(c *ui.Context, title string, perCore []float64, from, to int, tint ui.Color) {
	if title != "" {
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(c.Theme().TextMuted).Margin(g()/2, 0, 0, 0)
	}
	ui.Grid(c).Columns(2).GapX(3 * g()).GapY(g()).Children(func() {
		for i := from; i < to; i++ {
			p := perCore[i]
			components.BarRow(c, fmt.Sprintf("Core %d", i+1), ptr(p), pctShort(p), tint)
		}
	})
}

// activityCard lists the load averages with how busy they are for this
// many cores, the trend, and what the host is running.
func activityCard(c *ui.Context, v hub.HostView, now time.Time) {
	s := v.Snap
	components.Surface(c, ui.Column(c).Gap(g()+2)).Children(func() {
		overviewSectionHeader(c, icons.Activity, "Activity", "")
		known := !partial(s, "cpu")
		load := func(i int) string {
			if !known {
				return format.Unknown
			}
			return loadText(s.CPU.Load[i])
		}
		cores := len(s.CPU.PerCore)
		first := load(0)
		if known && cores > 0 {
			first += " · " + busyLabel(s.CPU.Load[0], cores)
		}
		row(c, "Load, last minute", first)
		row(c, "Load, 5 minutes", load(1))
		row(c, "Load, 15 minutes", load(2))
		if known {
			row(c, "Trend", trendLabel(s.CPU.Load[0], s.CPU.Load[2]))
		}
		if n := len(v.Procs); n > 0 {
			threads := 0
			for _, p := range v.Procs {
				threads += int(p.Threads)
			}
			row(c, "Processes", thousands(n))
			if threads > 0 {
				row(c, "Threads", thousands(threads))
			}
		}
		if s.Uptime > 0 {
			row(c, "Uptime", format.Duration(s.Uptime))
		}
		if s.Battery != nil && s.Battery.ThermalState != "" {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Thermal").FontSize(13).TextColor(c.Theme().TextMuted).Grow(1).SingleLine()
				components.Badge(c, s.Battery.ThermalState)
			})
		}
	})
}

// row is a muted label left and a value right.
func row(c *ui.Context, label, value string) {
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(c.Theme().TextMuted).Grow(1).SingleLine()
		ui.Text(c, value).FontSize(13).FontWeight(600).SingleLine()
	})
}

// topCPUCard lists the busiest processes in two columns, with a link to
// the full list.
func topCPUCard(c *ui.Context, v hub.HostView) {
	if len(v.Procs) == 0 {
		note(c, noProcs)
		return
	}
	t := c.Theme()
	top := slices.Clone(v.Procs)
	slices.SortStableFunc(top, func(a, b model.Process) int { return cmp.Compare(b.CPUPct, a.CPUPct) })
	if len(top) > topN {
		top = top[:topN]
	}
	components.Surface(c, ui.Column(c).Gap(g())).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			overviewSectionHeader(c, icons.CPU, "Using the Most CPU", "cpu")
			ui.Spacer(c)
			if navigate != nil {
				link := ui.Row(c).Key("cpu-show-all").Gap(4).AlignItems(ui.Center).Label("Show All")
				if link.Clicked() {
					navigate("processes")
				}
				link.Children(func() {
					ui.Text(c, "Show All").FontSize(13).FontWeight(600).TextColor(t.Accent).SingleLine()
					components.Glyph(c, icons.ChevronRight, 11, t.Accent)
				})
			}
		})
		ui.Grid(c).Columns(2).GapX(4 * g()).GapY(g()).Children(func() {
			for i, p := range top {
				ui.Row(c).Key(fmt.Sprintf("top-%d", i)).Gap(g()).AlignItems(ui.Center).Children(func() {
					procIcon(c, &p)
					ui.Text(c, p.Name).FontSize(13).SingleLine().Grow(1)
					col := t.Text
					if p.CPUPct >= 50 {
						col = t.Warning
					}
					ui.Text(c, pctShort(p.CPUPct)).FontSize(13).FontWeight(600).TextColor(col).SingleLine()
				})
			}
		})
	})
}

// pctShort is "66%" from 10 up, "6.9%" below.
func pctShort(v float64) string {
	if v >= 10 {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}

// degrees is "54 °C".
func degrees(c float64) string { return fmt.Sprintf("%.0f °C", c) }

// loadText is "7.08" under 10, "12.3" from 10.
func loadText(l float64) string {
	if l >= 10 {
		return fmt.Sprintf("%.1f", l)
	}
	return fmt.Sprintf("%.2f", l)
}

// busyLabel says how busy a load average is for this many cores.
func busyLabel(load float64, cores int) string {
	switch r := load / float64(cores); {
	case r < 0.3:
		return "Quiet"
	case r < 0.6:
		return "Light"
	case r < 1:
		return "Busy"
	}
	return "Overloaded"
}

// trendLabel compares the last minute with the last 15.
func trendLabel(l1, l15 float64) string {
	switch {
	case l1 > l15*1.15 && l1-l15 > 0.2:
		return "Rising"
	case l1 < l15*0.85 && l15-l1 > 0.2:
		return "Falling"
	}
	return "Steady"
}

// thousands is n with comma separators: 4674 → "4,674".
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
