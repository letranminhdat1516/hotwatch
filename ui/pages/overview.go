// Package pages builds the content area of each page; ui.Shell builds the
// rest of the window around it.
package pages

import (
	"cmp"
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

// stat is one labelled figure in a card's stats row.
type stat struct{ label, value string }

// card is one Overview metric card (UI-DESIGN.md, "Metric card anatomy").
type card struct {
	key      string // metric key: icon tile colour, chart tint
	icon     *ui.SVG
	title    string
	sub      string // sub-label above the headline: "Now", "In use of 24.0 GB"
	value    string // headline number; format.Unknown when unknown
	unit     string
	badge    string // badge beside the headline (memory pressure); "" for none
	tone     string // badge tone: "ok", "warn" or "bad"
	stats    [3]stat
	series   string  // hub.History series for the chart; "" for none
	pctScale bool    // the chart's ceiling is 100
	layers   []layer // a stacked chart instead of series: front (bottom) first
}

// layer is one series of a card's stacked chart and its colour.
type layer struct {
	series string
	hex    string
}

// Overview builds the Overview page's content for one host: the metric
// cards in a three-column grid, the analytics cards after them, and the
// uptime.
func Overview(c *ui.Context, v hub.HostView, now time.Time) {
	s := v.Snap
	if s == nil {
		components.EmptyState(c, "Waiting for data…")
		return
	}
	g := float32(components.Grid)
	ui.Column(c).Gap(2 * g).Children(func() {
		ui.Grid(c).Columns(3).Gap(2 * g).Children(func() {
			for _, cd := range cards(v, now) {
				var pts []hub.Point
				if cd.series != "" && v.Hist != nil {
					pts = v.Hist.Series(cd.series)
				}
				overviewCard(c, cd, pts, v.Hist, now)
			}
			overviewExtras(c, v)
		})
		ui.Text(c, "Uptime "+format.Duration(s.Uptime)).FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

// overviewCard draws one metric card: the tinted icon and title with a
// chevron, the sub-label, the headline with its unit (and badge), the three
// stats, then the dot-matrix chart of pts over the window ending at now.
func overviewCard(c *ui.Context, cd card, pts []hub.Point, hist *hub.History, now time.Time) {
	t := c.Theme()
	tint := t.TextMuted
	if col, ok := components.MetricColor(cd.key); ok {
		tint = col
	}
	g := float32(components.Grid)
	components.Surface(c, ui.Column(c).Gap(g/2)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(g).Children(func() {
			if cd.icon != nil {
				components.HeaderTile(c, cd.icon, tint.Alpha(0.16), tint)
			}
			ui.Text(c, cd.title).FontSize(15).FontWeight(600).TextColor(tint).SingleLine().Grow(1)
			components.Glyph(c, icons.ChevronRight, 12, t.TextMuted)
		})
		if cd.sub != "" {
			ui.Text(c, cd.sub).FontSize(13).TextColor(t.TextMuted).SingleLine().Margin(g, 0, 0, 0)
		}
		ui.Row(c).AlignItems(ui.Center).Gap(g).Children(func() {
			spans := []ui.Span{{Text: cd.value, Size: 40, Weight: 700}}
			if cd.unit != "" && cd.value != format.Unknown {
				spans = append(spans, ui.Span{Text: " " + cd.unit, Size: 16, Weight: 500, Color: t.TextMuted})
			}
			ui.RichText(c, spans...).SingleLine().Grow(1)
			if cd.badge != "" {
				pill(c, cd.badge, cd.tone)
			}
		})
		ui.Row(c).Gap(g).Margin(g/2, 0, 0, 0).Children(func() {
			for _, st := range cd.stats {
				col := ui.Column(c).Grow(1).Gap(2)
				if st.label == "" {
					continue // an empty slot keeps the other columns' widths
				}
				col.Children(func() {
					ui.Text(c, st.label).FontSize(12).TextColor(t.TextMuted).SingleLine()
					ui.Text(c, st.value).FontSize(15).FontWeight(600).SingleLine()
				})
			}
		})
		if cd.series != "" || len(cd.layers) > 0 {
			ui.Spacer(c) // charts line up along the bottom of a row
			ceiling := 0.0
			if cd.pctScale {
				ceiling = 100
			}
			from := now.Add(-components.ChartWindow())
			if ls := stackLayers(hist, cd.layers); len(ls) > 0 {
				components.StackedSparkline(c, ls, from, now, ceiling, tint).Margin(g, 0, 0, 0)
			} else {
				components.Sparkline(c, pts, from, now, ceiling, tint).Margin(g, 0, 0, 0)
			}
		}
	})
}

// stackLayers resolves a card's layers from the history; nil when any
// layer has no points yet (the plain sparkline stands in).
func stackLayers(h *hub.History, ls []layer) []components.DotLayer {
	if h == nil || len(ls) == 0 {
		return nil
	}
	out := make([]components.DotLayer, 0, len(ls))
	for _, l := range ls {
		pts := h.Series(l.series)
		if len(pts) == 0 {
			return nil
		}
		out = append(out, components.DotLayer{Pts: pts, Color: ui.Hex(l.hex)})
	}
	return out
}

// pill is a small tinted status badge.
func pill(c *ui.Context, text, tone string) {
	t := c.Theme()
	col := t.Success
	switch tone {
	case "warn":
		col = t.Warning
	case "bad":
		col = t.Danger
	}
	ui.Row(c).Padding(3, 8).Radius(10).Background(col.Alpha(0.14)).Children(func() {
		ui.Text(c, text).FontSize(12).FontWeight(600).TextColor(col).SingleLine()
	})
}

// overviewExtras adds the analytics cards after the metric cards, in the
// same grid: Memory by Type, Memory by App and Energy by App, each only
// when its data is available for this host.
func overviewExtras(c *ui.Context, v hub.HostView) {
	s := v.Snap
	if !partial(s, "memory") && s.Memory.Total > 0 {
		memoryByType(c, s.Memory, v.Info.OS)
	}
	if apps := byApp(v.Procs); len(apps) > 0 {
		memoryByApp(c, apps)
	}
	if apps := energyByApp(v.Procs); len(apps) > 0 {
		energyCard(c, apps)
	}
}

// memoryByType is the donut of the memory parts with "83% in use" in its
// middle; on a Mac, cached files are drawn in grey within what is free, and
// Free is everything not in use, as Activity Monitor counts it.
func memoryByType(c *ui.Context, m model.Memory, os string) {
	t := c.Theme()
	g := float32(components.Grid)
	segs := segments(m, os)
	free := uint64(0)
	if m.Total > m.Used {
		free = m.Total - m.Used
	}
	ring, legend := segs, segs
	if os == "darwin" && m.Cached > 0 && len(segs) > 0 {
		cached := min(m.Cached, free)
		parts := segs[:len(segs)-1] // App, Wired, Compressed
		ring = append(append([]components.Segment{}, parts...),
			components.Segment{Label: "Cached", Value: cached, Shade: true},
			components.Segment{Label: "Free", Value: free - cached, Muted: true})
		legend = append(append([]components.Segment{}, parts...),
			components.Segment{Label: "Cached", Value: m.Cached, Shade: true},
			components.Segment{Label: "Free", Value: free, Muted: true})
	}
	colors := components.SegColors(t, legend)
	components.Surface(c, ui.Column(c).Gap(g)).Children(func() {
		overviewSectionHeader(c, icons.Memory, "Memory by Type", "memory")
		ui.Row(c).Gap(3*g).AlignItems(ui.Center).Margin(g, 0, 0, 0).Children(func() {
			components.Donut(c, ring, 16*g).Center().Children(func() {
				ui.Text(c, fmt.Sprintf("%.0f%%", float64(m.Used)/float64(m.Total)*100)).FontSize(26).FontWeight(700).SingleLine()
				ui.Text(c, "in use").FontSize(12).TextColor(t.TextMuted).SingleLine()
			})
			ui.Column(c).Gap(g - 2).Grow(1).Children(func() {
				for i, sg := range legend {
					ui.Row(c).Gap(g).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(10, 10).Radius(5).Background(colors[i])
						ui.Text(c, sg.Label).FontSize(14).Grow(1).NoWrap()
						ui.Text(c, format.Bytes(sg.Value)).FontSize(14).TextColor(t.TextMuted).NoWrap()
					})
				}
			})
		})
	})
}

// memoryByApp is a mosaic of the largest apps: the first two as tall
// tiles, the next one wide, then small tiles with only their icon.
func memoryByApp(c *ui.Context, apps []app) {
	t := c.Theme()
	g := float32(components.Grid)
	var sum uint64
	for _, a := range apps {
		sum += a.rss
	}
	components.Surface(c, ui.Column(c).Gap(g)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			overviewSectionHeader(c, icons.Apps, "Memory by App", "memory")
			ui.Spacer(c)
			ui.Text(c, format.Bytes(sum)+" in all").FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
		ui.Grid(c).Columns(4).RowTracks(ui.Fixed(84), ui.Fixed(84)).Gap(g).Margin(g/2, 0, 0, 0).Children(func() {
			for i, a := range apps[:min(len(apps), 7)] {
				tile := ui.Column(c).Key(fmt.Sprintf("tile-%d", i)).Padding(10).Radius(12).Gap(2).
					Background(t.SurfaceHover).Label(a.name)
				big := i < 3
				switch i {
				case 0, 1:
					tile.RowSpan(2)
				case 2:
					tile.ColumnSpan(2)
				}
				tile.Children(func() {
					appIcon(c, a.name, a.bundle)
					if big {
						ui.Spacer(c)
						ui.Text(c, a.name).FontSize(13).FontWeight(600).SingleLine()
						ui.Text(c, format.Bytes(a.rss)).FontSize(12).TextColor(t.TextMuted).SingleLine()
					}
				})
			}
		})
	})
}

// powerApp is one app's share of the power the processes draw.
type powerApp struct {
	name, bundle string
	mw           float64
}

// energyByApp groups the processes' power by app (helpers under their
// app) and returns the five largest; nil when no process reports power.
func energyByApp(ps []model.Process) []powerApp {
	idx := map[string]int{}
	var out []powerApp
	for _, p := range ps {
		if p.PowerMW == nil || *p.PowerMW <= 0 {
			continue
		}
		key, name, bundle := p.Name, p.Name, ""
		if p.AppBundle != "" {
			bundle = outerBundle(p.AppBundle)
			key, name = bundle, strings.TrimSuffix(path.Base(bundle), ".app")
		}
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			out = append(out, powerApp{name: name, bundle: bundle})
		}
		out[i].mw += *p.PowerMW
	}
	slices.SortStableFunc(out, func(a, b powerApp) int { return cmp.Compare(b.mw, a.mw) })
	return out
}

// watts is "437 mW" under a watt, "1.26 W" from one.
func watts(mw float64) string {
	if mw < 1000 {
		return fmt.Sprintf("%.0f mW", mw)
	}
	return fmt.Sprintf("%.2f W", mw/1000)
}

// energyCard lists the five apps drawing the most power with a bar each.
func energyCard(c *ui.Context, apps []powerApp) {
	t := c.Theme()
	g := float32(components.Grid)
	tint := metricTint(c, "energy")
	var sum float64
	for _, a := range apps {
		sum += a.mw
	}
	top := apps[:min(len(apps), 5)]
	components.Surface(c, ui.Column(c).Gap(g)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			overviewSectionHeader(c, icons.Energy, "Energy by App", "energy")
			ui.Spacer(c)
			ui.Text(c, watts(sum)+" in all").FontSize(12).TextColor(t.TextMuted).SingleLine()
		})
		for i, a := range top {
			fill := float32(a.mw / top[0].mw)
			ui.Row(c).Key(fmt.Sprintf("energy-%d", i)).Gap(g).AlignItems(ui.Center).Children(func() {
				appIcon(c, a.name, a.bundle)
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Row(c).Children(func() {
						ui.Text(c, a.name).FontSize(13).SingleLine().Grow(1)
						ui.Text(c, watts(a.mw)).FontSize(13).TextColor(t.TextMuted).SingleLine()
					})
					ui.Box(c).Height(5).Radius(2.5).Background(tint.Alpha(0.18)).Draw(func(p *ui.Painter, r ui.Rect) {
						p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*fill, 5), H: r.H}, tint, 2.5)
					})
				})
			})
		}
	})
}

// overviewSectionHeader is the title row of an analytics card: a tinted
// icon tile and the title in the metric colour, or neutral when key has none.
func overviewSectionHeader(c *ui.Context, icon *ui.SVG, title, key string) {
	t := c.Theme()
	tint, tileBg, iconCol := t.Text, t.SurfaceHover, t.TextMuted
	if col, ok := components.MetricColor(key); ok {
		tint, tileBg, iconCol = col, col.Alpha(0.14), col
	}
	ui.Row(c).AlignItems(ui.Center).Gap(components.Grid).Children(func() {
		components.HeaderTile(c, icon, tileBg, iconCol)
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(tint).SingleLine()
	})
}

// gpuSeries is the History series of GPU i. History records only the first
// GPU (as "gpu"); the others have no series yet.
func gpuSeries(i int) string {
	if i == 0 {
		return "gpu"
	}
	return fmt.Sprintf("gpu.%d", i)
}

func partial(s *model.Snapshot, section string) bool { return slices.Contains(s.Partial, section) }

func ptr(v float64) *float64 { return &v }

// sumRate adds the known rates; nil when none is known.
func sumRate(rates []*float64) *float64 {
	var total float64
	ok := false
	for _, r := range rates {
		if r != nil {
			total += *r
			ok = true
		}
	}
	if !ok {
		return nil
	}
	return &total
}

// splitUnit separates "19.0 GB" into "19.0" and "GB" so the headline can
// set them in different sizes; a value without a space has no unit.
func splitUnit(s string) (num, unit string) {
	if i := strings.LastIndexByte(s, ' '); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// pressureBadge maps the memory pressure level to its badge text and tone.
func pressureBadge(level string) (text, tone string) {
	switch level {
	case "normal":
		return "Normal", "ok"
	case "warn":
		return "Elevated", "warn"
	case "critical":
		return "Critical", "bad"
	}
	return "", ""
}

// cards builds the metric cards for v's latest snapshot. Unknown figures show
// format.Unknown, never 0. Session totals and window statistics come from
// v.Hist when it has them.
func cards(v hub.HostView, now time.Time) []card {
	s := v.Snap
	from := now.Add(-components.ChartWindow())
	unknown3 := func(a, b, c string) [3]stat {
		return [3]stat{{a, format.Unknown}, {b, format.Unknown}, {c, format.Unknown}}
	}
	var out []card

	cpu := card{key: "cpu", icon: icons.CPU, title: "CPU", sub: "Now", value: format.Unknown, unit: "%",
		stats: unknown3("User", "System", "Load 1m"), series: "cpu", pctScale: true,
		layers: []layer{{"cpu.system", cpuSystemHex}, {"cpu", metricHex("cpu")}}}
	if s.CPU.TempC != nil {
		cpu.stats[2].label = "Temperature"
	}
	if !partial(s, "cpu") {
		cpu.value = whole(s.CPU.Total)
		if s.CPU.User != nil {
			cpu.stats[0].value = pctShort(*s.CPU.User)
		}
		if s.CPU.System != nil {
			cpu.stats[1].value = pctShort(*s.CPU.System)
		}
		if s.CPU.TempC != nil {
			cpu.stats[2].value = degrees(*s.CPU.TempC)
		} else {
			cpu.stats[2].value = fmt.Sprintf("%.2f", s.CPU.Load[0])
		}
	}
	out = append(out, cpu)

	mem := card{key: "memory", icon: icons.Memory, title: "Memory", value: format.Unknown,
		stats: unknown3("App", "Wired", "Compressed"), series: "mem", pctScale: true,
		layers: []layer{{"mem.wired", "#ff9f0a"}, {"mem.wiredcomp", "#64d2ff"}, {"mem", metricHex("memory")}}}
	if !partial(s, "memory") && s.Memory.Total > 0 {
		mem.sub = "In use of " + format.Bytes(s.Memory.Total)
		mem.value, mem.unit = splitUnit(format.Bytes(s.Memory.Used))
		i := 0
		for _, sg := range segments(s.Memory, v.Info.OS) {
			if sg.Muted || i >= len(mem.stats) {
				continue
			}
			mem.stats[i] = stat{sg.Label, format.Bytes(sg.Value)}
			i++
		}
		for ; i < len(mem.stats); i++ {
			mem.stats[i] = stat{}
		}
		mem.badge, mem.tone = pressureBadge(s.Memory.Pressure)
	}
	out = append(out, mem)

	for i, gp := range s.GPUs {
		name := gp.Name
		if name == "" {
			name = fmt.Sprintf("GPU %d", i+1)
		}
		cd := card{key: "gpu", icon: icons.GPU, title: "GPU", sub: name, value: format.Unknown, unit: "%",
			stats: unknown3("Memory", "Average", "Peak"), series: gpuSeries(i), pctScale: true}
		if !partial(s, "gpu") {
			if gp.UtilPct != nil {
				cd.value = whole(*gp.UtilPct)
			}
			if gp.MemUsed != nil {
				cd.stats[0].value = format.Bytes(*gp.MemUsed)
			}
			if v.Hist != nil {
				if avg, peak, ok := v.Hist.Stats(cd.series, from); ok {
					cd.stats[1].value = fmt.Sprintf("%.0f%%", avg)
					cd.stats[2].value = fmt.Sprintf("%.0f%%", peak)
				}
			}
		}
		out = append(out, cd)
	}

	disk := card{key: "disk", icon: icons.Disk, title: "Disk", value: format.Unknown,
		stats: unknown3("Reading", "Writing", "Written"), series: "disk.write",
		layers: []layer{{"disk.write", "#64d2ff"}, {"disk.rw", metricHex("disk")}}}
	if !partial(s, "disks") && len(s.Disks) > 0 {
		var used, total uint64
		var reads, writes []*float64
		for _, d := range model.DistinctVolumes(s.Disks) {
			used += d.Used
			total += d.Total
			reads, writes = append(reads, d.ReadBps), append(writes, d.WriteBps)
		}
		if total > 0 {
			disk.sub = "Free of " + format.Bytes(total)
			disk.value, disk.unit = splitUnit(format.Bytes(total - min(used, total)))
		}
		disk.stats[0].value = format.Rate(sumRate(reads))
		disk.stats[1].value = format.Rate(sumRate(writes))
		if v.Hist != nil {
			if w, ok := v.Hist.Total("disk.write"); ok {
				disk.stats[2].value = format.Bytes(uint64(w))
			}
		}
	}
	out = append(out, disk)

	net := card{key: "network", icon: icons.Network, title: "Network", sub: "Downloading", value: format.Unknown,
		stats: unknown3("Uploading", "Received", "Sent"), series: "net.rx",
		layers: []layer{{"net.tx", "#0a84ff"}, {"net.rxtx", metricHex("network")}}}
	if !partial(s, "net") {
		var rxs, txs []*float64
		for _, i := range s.Net {
			if i.Name == "lo" || i.Name == "lo0" || !i.Up {
				continue
			}
			rxs, txs = append(rxs, i.RxBps), append(txs, i.TxBps)
		}
		net.value, net.unit = splitUnit(format.Rate(sumRate(rxs)))
		net.stats[0].value = format.Rate(sumRate(txs))
		if v.Hist != nil {
			if r, ok := v.Hist.Total("net.rx"); ok {
				net.stats[1].value = format.Bytes(uint64(r))
			}
			if tx, ok := v.Hist.Total("net.tx"); ok {
				net.stats[2].value = format.Bytes(uint64(tx))
			}
		}
	}
	out = append(out, net)

	if b := s.Battery; b != nil {
		bat := card{key: "battery", icon: icons.Battery, title: "Battery", value: format.Unknown, unit: "%",
			stats: unknown3("Remaining", "Power Draw", "Temperature"), series: "battery", pctScale: true}
		if b.Charging {
			bat.stats[0].label = "Until Full"
		}
		if b.HealthPct != nil {
			bat.stats[2].label = "Health"
		}
		if !partial(s, "battery") {
			bat.value = whole(b.ChargePct)
			bat.sub = "On battery"
			switch {
			case b.Charging:
				bat.sub = "Charging"
			case b.OnAC:
				bat.sub = "On AC"
			}
			if b.TimeRemaining != nil {
				bat.stats[0].value = format.Duration(*b.TimeRemaining)
			}
			if n := format.Number(b.PowerW); n != format.Unknown {
				bat.stats[1].value = n + " W"
			}
			switch {
			case b.HealthPct != nil:
				bat.stats[2].value = fmt.Sprintf("%.0f%%", *b.HealthPct)
			case b.TempC != nil:
				bat.stats[2].value = degrees(*b.TempC)
			}
		}
		out = append(out, bat)
	}
	return out
}

// whole is a percentage headline without decimals: "34".
func whole(v float64) string { return fmt.Sprintf("%.0f", v) }

// metricHex is the hex of a metric colour, for chart layers.
func metricHex(key string) string {
	col, _ := components.MetricColor(key)
	return fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B)
}

// ratio is used/total in percent; nil when total is 0.
func ratio(used, total uint64) *float64 {
	if total == 0 {
		return nil
	}
	return ptr(float64(used) / float64(total) * 100)
}
