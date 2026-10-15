package pages

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// segments splits memory into the parts the platform reports: App, Wired and
// Compressed where those are set (macOS), otherwise Used, Cached and
// Buffers (Linux); Info.OS decides only if both are set. Free is what the
// parts leave of Total.
func segments(m model.Memory, os string) []components.Segment {
	mac := m.App+m.Wired+m.Compressed > 0
	linux := m.Cached+m.Buffers > 0
	if mac && linux {
		mac = os == "darwin"
	}
	var segs []components.Segment
	switch {
	case mac:
		segs = []components.Segment{{Label: "App", Value: m.App}, {Label: "Wired", Value: m.Wired}, {Label: "Compressed", Value: m.Compressed}}
	case linux:
		segs = []components.Segment{{Label: "Used", Value: m.Used}, {Label: "Cached", Value: m.Cached}, {Label: "Buffers", Value: m.Buffers}}
	default:
		segs = []components.Segment{{Label: "Used", Value: m.Used}}
	}
	var sum uint64
	for _, s := range segs {
		sum += s.Value
	}
	free := uint64(0)
	if m.Total > sum {
		free = m.Total - sum
	}
	return append(segs, components.Segment{Label: "Free", Value: free, Muted: true})
}

// Memory colours, shared with the overview's stacked chart: App in the
// memory green, Wired orange, Compressed light blue, Cached grey.
var (
	wiredHex = "#ff9f0a"
	compHex  = "#64d2ff"
)

// Memory builds the Memory page (the owner's reference): the memory in use
// with its parts as a bar and a stacked dot chart, then Memory by Type,
// Pressure and Swap side by side, Memory by App as a mosaic and the
// processes using the most memory.
func Memory(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	known := !partial(s, "memory") && s.Memory.Total > 0
	pageColumn(c, func() {
		memoryCard(c, v, known, now)
		if known {
			ui.Grid(c).Columns(3).Gap(2 * g()).Children(func() {
				memoryByType(c, s.Memory, v.Info.OS, "")
				pressureCard(c, s.Memory)
				swapCard(c, s.Memory)
			})
		}
		if apps := byApp(v.Procs); len(apps) > 0 && v.Info.OS == "darwin" {
			memoryByApp(c, apps, "", true)
		}
		topMemoryCard(c, v)
	})
}

// memoryCard is the headline (in use of the total), its parts as figures, a
// bar and the stacked chart of the chart window.
func memoryCard(c *ui.Context, v hub.HostView, known bool, now time.Time) {
	t := c.Theme()
	s := v.Snap
	m := s.Memory
	tint := metricTint(c, "memory")
	wired, comp := ui.Hex(wiredHex), ui.Hex(compHex)
	mac := m.App+m.Wired+m.Compressed > 0 && v.Info.OS != "linux"
	right := func() {
		if mac {
			legend(c, tint, "App")
			legend(c, comp, "Compressed")
			legend(c, wired, "Wired")
		}
		if known && m.Pressure != "" && m.Pressure != "unknown" {
			label, tone := pressureBadge(m.Pressure)
			statusPill(c, label, tone)
		}
	}
	pageCard(c, icons.Memory, "Memory", "memory", right, func() {
		if !known {
			headline(c, "In use", format.Unknown, "", nil)
			return
		}
		val, unit := splitUnit(format.Bytes(m.Used))
		var figs []fig
		var parts []part
		if mac {
			figs = []fig{{"App", format.Bytes(m.App), ""}, {"Wired", format.Bytes(m.Wired), ""},
				{"Compressed", format.Bytes(m.Compressed), ""}}
			parts = []part{{float64(m.App), tint}, {float64(m.Wired), wired}, {float64(m.Compressed), comp}}
			if m.Cached > 0 {
				figs = append(figs, fig{"Cached", format.Bytes(m.Cached), ""})
				parts = append(parts, part{float64(min(m.Cached, m.Total-min(m.Used, m.Total))), components.ShadeColor(t)})
			}
		} else {
			figs = []fig{{"Used", format.Bytes(m.Used), ""}, {"Cached", format.Bytes(m.Cached), ""}, {"Buffers", format.Bytes(m.Buffers), ""}}
			parts = []part{{float64(m.Used), tint}, {float64(m.Cached + m.Buffers), components.ShadeColor(t)}}
		}
		headline(c, "In use of "+format.Bytes(m.Total), val, unit, figs)
		segBar(c, parts, float64(m.Total), t.Border, 8).Margin(g(), 0, 0, 0)
		from := now.Add(-components.ChartWindow())
		layers := []components.DotLayer{{Pts: points(v, "mem"), Color: tint}}
		if w, wc := points(v, "mem.wired"), points(v, "mem.wiredcomp"); len(w) > 0 && len(wc) > 0 {
			layers = []components.DotLayer{{Pts: w, Color: wired}, {Pts: wc, Color: comp}, {Pts: points(v, "mem"), Color: tint}}
		}
		components.StackedDotChart(c, layers, from, now, 100, tint, 200).Margin(g(), 0, 0, 0)
	})
}

// pressureCard is how hard the system works to keep memory free, as a
// ring, with the paging that shows it.
func pressureCard(c *ui.Context, m model.Memory) {
	t := c.Theme()
	label, tone := pressureBadge(m.Pressure)
	col := metricTint(c, "memory")
	switch tone {
	case "warn":
		col = t.Warning
	case "bad":
		col = t.Danger
	}
	right := func() {
		if m.Pressure != "" && m.Pressure != "unknown" {
			statusPill(c, label, tone)
		}
	}
	pageCard(c, icons.Activity, "Pressure", "memory", right, func() {
		ui.Row(c).Justify(ui.Center).Margin(g(), 0, g(), 0).Children(func() {
			if m.PressurePct != nil {
				components.Ring(c, *m.PressurePct/100, 18*g(), col, fmt.Sprintf("%.0f%%", *m.PressurePct), "pressure")
			} else {
				components.Ring(c, 0, 18*g(), col, label, "pressure")
			}
		})
		ui.Spacer(c)
		kvRow(c, "Page-ins", perSecond(m.PageInsPS))
		kvRow(c, "Swap-outs", perSecond(m.SwapOutsPS))
	})
}

// swapCard is what macOS has written to its swap files.
func swapCard(c *ui.Context, m model.Memory) {
	t := c.Theme()
	blue := ui.Hex(compHex)
	pageCard(c, icons.Swap, "Swap", "", nil, func() {
		if m.SwapTotal == 0 {
			headline(c, "Written to disk", "0", "B", nil)
			note(c, "Swap is off or empty.")
			return
		}
		val, unit := splitUnit(format.Bytes(m.SwapUsed))
		headline(c, "Written to disk", val, unit, nil)
		segBar(c, []part{{float64(m.SwapUsed), blue}}, float64(m.SwapTotal), blue.Alpha(0.18), 8).Margin(g(), 0, g(), 0)
		ui.Spacer(c)
		kvRow(c, "Swap size", format.Bytes(m.SwapTotal))
		if m.Compressed > 0 {
			kvRow(c, "Compressed", format.Bytes(m.Compressed))
		}
		_ = t
	})
}

// topMemoryCard lists the apps using the most memory, with a link to the
// whole process list.
func topMemoryCard(c *ui.Context, v hub.HostView) {
	if len(v.Procs) == 0 {
		note(c, noProcs)
		return
	}
	apps := groupByApp(v.Procs, func(p model.Process) (float64, bool) { return float64(p.Mem()), p.Mem() > 0 })
	tint := metricTint(c, "memory")
	pageCard(c, icons.Memory, "Using the Most Memory", "memory", func() { showAll(c, "mem-show-all", "processes", tint) }, func() {
		topApps(c, apps, 10, func(b float64) string { return format.Bytes(uint64(b)) })
	})
}

// perSecond is "58 /s"; a dash when unknown.
func perSecond(v *float64) string {
	if v == nil {
		return format.Unknown
	}
	if *v >= 10 || *v == 0 {
		return fmt.Sprintf("%.0f /s", *v)
	}
	return fmt.Sprintf("%.1f /s", *v)
}

// app is the processes of one application bundle (or of one executable name
// when it has no bundle) and their combined resident memory.
type app struct {
	name   string
	bundle string // "" for a plain executable
	user   string // the first process's user
	procs  int
	rss    uint64
}

// byApp groups processes by application, falling back to Name for
// processes outside a bundle, and returns the topN groups by memory
// (footprint where known, else RSS). Helpers nested in an app (Chrome's
// renderers, under Google Chrome.app/…/Helpers/) count toward that app.
// Processes with no readable memory are skipped. A bundle is shown by its
// file name without ".app".
func byApp(ps []model.Process) []app {
	idx := map[string]int{}
	var apps []app
	for _, p := range ps {
		m := p.Mem()
		if m == 0 {
			continue
		}
		key, name, bundle := p.Name, p.Name, ""
		if p.AppBundle != "" {
			bundle = outerBundle(p.AppBundle)
			key = bundle
			name = strings.TrimSuffix(path.Base(bundle), ".app")
		}
		i, ok := idx[key]
		if !ok {
			i = len(apps)
			idx[key] = i
			apps = append(apps, app{name: name, bundle: bundle, user: p.User})
		}
		apps[i].procs++
		apps[i].rss += m
	}
	slices.SortStableFunc(apps, func(a, b app) int {
		switch {
		case a.rss > b.rss:
			return -1
		case a.rss < b.rss:
			return 1
		}
		return 0
	})
	if len(apps) > topN {
		apps = apps[:topN]
	}
	return apps
}

// outerBundle is the outermost *.app directory in a bundle path:
// "/Applications/A.app/Contents/Helpers/B.app" → "/Applications/A.app".
func outerBundle(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.Index(p, ".app/"); i >= 0 {
		return p[:i+len(".app")]
	}
	return p
}
