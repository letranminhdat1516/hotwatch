// Package pages builds the content area of each page; ui.Shell builds the
// rest of the window around it.
package pages

import (
	"fmt"
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
	series   string // hub.History series for the chart; "" for none
	pctScale bool   // the chart's ceiling is 100
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
				overviewCard(c, cd, pts, now)
			}
			overviewExtras(c, v)
		})
		ui.Text(c, "Uptime "+format.Duration(s.Uptime)).FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

// overviewCard draws one metric card: the tinted icon and title with a
// chevron, the sub-label, the headline with its unit (and badge), the three
// stats, then the dot-matrix chart of pts over the window ending at now.
func overviewCard(c *ui.Context, cd card, pts []hub.Point, now time.Time) {
	t := c.Theme()
	tint := t.TextMuted
	if col, ok := components.MetricColor(cd.key); ok {
		tint = col
	}
	g := float32(components.Grid)
	components.Surface(c, ui.Column(c).Gap(g/2)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(g).Children(func() {
			if cd.icon != nil {
				components.IconTile(c, cd.icon, tint.Alpha(0.14), tint)
			}
			ui.Text(c, cd.title).FontSize(15).FontWeight(600).TextColor(tint).SingleLine().Grow(1)
			ui.Icon(c, icons.ChevronRight).FontSize(14).TextColor(t.TextMuted)
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
		if cd.series != "" {
			ui.Spacer(c) // charts line up along the bottom of a row
			ceiling := 0.0
			if cd.pctScale {
				ceiling = 100
			}
			components.Sparkline(c, pts, now.Add(-components.ChartWindow), now, ceiling, tint).Margin(g, 0, 0, 0)
		}
	})
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
// same grid: Memory by Type (donut) and Memory by App, each only when its
// data is available for this host. Energy by App needs per-process power
// from the collector and is omitted until it reports it.
func overviewExtras(c *ui.Context, v hub.HostView) {
	s := v.Snap
	t := c.Theme()
	g := float32(components.Grid)
	if !partial(s, "memory") && s.Memory.Total > 0 {
		segs := segments(s.Memory, v.Info.OS)
		components.Surface(c, ui.Column(c).Gap(g)).Children(func() {
			overviewSectionHeader(c, icons.Memory, "Memory by Type", "memory")
			ui.Row(c).Gap(2 * g).AlignItems(ui.Center).Children(func() {
				components.Donut(c, segs, 14*g)
				components.SegLegend(c, segs)
			})
		})
	}
	if apps := byApp(v.Procs); len(apps) > 0 {
		var maxRSS, sum uint64
		for _, a := range apps {
			maxRSS = max(maxRSS, a.rss)
			sum += a.rss
		}
		components.Surface(c, ui.Column(c).Gap(g)).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				overviewSectionHeader(c, icons.Apps, "Memory by App", "")
				ui.Spacer(c)
				ui.Text(c, format.Bytes(sum)+" in all").FontSize(12).TextColor(t.TextMuted).SingleLine()
			})
			ui.Column(c).Gap(g - 2).Children(func() {
				for _, a := range apps {
					fill := float32(0)
					if maxRSS > 0 {
						fill = float32(a.rss) / float32(maxRSS)
					}
					ui.Row(c).Key(a.name).Gap(g).AlignItems(ui.Center).Children(func() {
						ui.Text(c, a.name).FontSize(12).TextColor(t.Text).Width(16 * g).SingleLine()
						ui.Box(c).Grow(1).Height(8).Radius(4).Background(t.Border).Draw(func(p *ui.Painter, r ui.Rect) {
							if fill > 0 {
								p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*fill, 8), H: r.H}, t.Accent, 4)
							}
						})
						ui.Text(c, format.Bytes(a.rss)).FontSize(12).FontWeight(600).Width(10 * g).TextAlign(ui.End).SingleLine()
					})
				}
			})
		})
	}
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
		components.IconTile(c, icon, tileBg, iconCol)
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
		return "Warning", "warn"
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
	from := now.Add(-components.ChartWindow)
	unknown3 := func(a, b, c string) [3]stat {
		return [3]stat{{a, format.Unknown}, {b, format.Unknown}, {c, format.Unknown}}
	}
	var out []card

	cpu := card{key: "cpu", icon: icons.CPU, title: "CPU", sub: "Now", value: format.Unknown, unit: "%",
		stats: unknown3("Load 1m", "Load 5m", "Load 15m"), series: "cpu", pctScale: true}
	if !partial(s, "cpu") {
		cpu.value = format.Number(ptr(s.CPU.Total))
		for i := range cpu.stats {
			cpu.stats[i].value = fmt.Sprintf("%.2f", s.CPU.Load[i])
		}
	}
	out = append(out, cpu)

	mem := card{key: "memory", icon: icons.Memory, title: "Memory", value: format.Unknown,
		stats: unknown3("App", "Wired", "Compressed"), series: "mem", pctScale: true}
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
			cd.value = format.Number(gp.UtilPct)
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
		stats: unknown3("Reading", "Writing", "Written"), series: "disk.write"}
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
		stats: unknown3("Uploading", "Received", "Sent"), series: "net.rx"}
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
		bat := card{key: "battery", icon: icons.Energy, title: "Battery", value: format.Unknown, unit: "%",
			stats: unknown3("Remaining", "Power draw", "Temperature"), series: "battery", pctScale: true}
		if !partial(s, "battery") {
			bat.value = format.Number(ptr(b.ChargePct))
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
			if n := format.Number(b.TempC); n != format.Unknown {
				bat.stats[2].value = n + " °C"
			}
		}
		out = append(out, bat)
	}
	return out
}

// ratio is used/total in percent; nil when total is 0.
func ratio(used, total uint64) *float64 {
	if total == 0 {
		return nil
	}
	return ptr(float64(used) / float64(total) * 100)
}
