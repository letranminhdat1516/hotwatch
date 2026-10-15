// Package pages builds the content area of each page; ui.Shell builds the
// rest of the window around it.
package pages

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// card is one metric card: a coloured icon and title, a headline value and
// its unit, detail lines, and an optional sparkline.
type card struct {
	key     string // metric key for icon and colour
	icon    *ui.SVG
	title   string
	value   string // headline number; format.Unknown when not known, "" for none
	unit    string
	details []string
	series  string // hub.History series name; "" for none
}

// Overview builds the Overview page's content for one host.
func Overview(c *ui.Context, v hub.HostView, now time.Time) {
	s := v.Snap
	if s == nil {
		components.EmptyState(c, "Waiting for data…")
		return
	}
	g := float32(components.Grid)
	ui.Column(c).Gap(2 * g).Children(func() {
		ui.Row(c).Gap(2 * g).Wrap().AlignItems(ui.Stretch).Children(func() {
			for _, cd := range cards(s) {
				var series []float64
				if cd.series != "" && v.Hist != nil {
					series = recent(v.Hist.Series(cd.series), now)
				}
				overviewCard(c, cd, series)
			}
		})
		overviewExtras(c, v)
		ui.Text(c, "Uptime "+format.Duration(s.Uptime)).FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

// overviewExtras is the analytics row under the metric cards: a Memory by
// Type donut and a Memory by App breakdown, each shown only when its data is
// available for this host.
func overviewExtras(c *ui.Context, v hub.HostView) {
	s := v.Snap
	t := c.Theme()
	memKnown := !partial(s, "memory") && s.Memory.Total > 0
	apps := byApp(v.Procs)
	if !memKnown && len(apps) == 0 {
		return
	}
	g := float32(components.Grid)
	ui.Row(c).Gap(2 * g).Wrap().AlignItems(ui.Stretch).Children(func() {
		if memKnown {
			segs := segments(s.Memory, v.Info.OS)
			components.Surface(c, ui.Column(c).Width(46 * g).Gap(g)).Children(func() {
				overviewSectionHeader(c, icons.Memory, "Memory by Type", "memory")
				ui.Row(c).Gap(2 * g).AlignItems(ui.Center).Children(func() {
					components.Donut(c, segs, 15*g)
					components.SegLegend(c, segs)
				})
			})
		}
		if len(apps) > 0 {
			var maxRSS uint64
			for _, a := range apps {
				maxRSS = max(maxRSS, a.rss)
			}
			components.Surface(c, ui.Column(c).Width(46 * g).Gap(g)).Children(func() {
				overviewSectionHeader(c, icons.Apps, "Memory by App", "")
				ui.Column(c).Gap(g - 2).Children(func() {
					for _, a := range apps {
						pct := 0.0
						if maxRSS > 0 {
							pct = float64(a.rss) / float64(maxRSS) * 100
						}
						ui.Row(c).Key(a.name).Gap(g).AlignItems(ui.Center).Children(func() {
							ui.Text(c, a.name).FontSize(12).TextColor(t.Text).Width(16 * g).SingleLine()
							track := ui.Box(c).Grow(1).Height(8).Radius(4).Background(t.Border)
							fill := min(max(pct/100, 0), 1)
							track.Draw(func(p *ui.Painter, r ui.Rect) {
								if fill > 0 {
									p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*float32(fill), 8), H: r.H}, t.Accent, 4)
								}
							})
							ui.Text(c, format.Bytes(a.rss)).FontSize(12).FontWeight(600).Width(10 * g).TextAlign(ui.End).SingleLine()
						})
					}
				})
			})
		}
	})
}

// overviewSectionHeader is the title row of an analytics card: a tinted icon
// tile and the title in the metric colour (or plain when key has none).
func overviewSectionHeader(c *ui.Context, icon *ui.SVG, title, key string) {
	t := c.Theme()
	tint := t.Text
	if col, ok := components.MetricColor(key); ok {
		tint = col
	}
	tileBg := t.SurfaceHover
	iconCol := t.TextMuted
	if col, ok := components.MetricColor(key); ok {
		tileBg, iconCol = col, ui.Hex("#ffffff")
	}
	ui.Row(c).AlignItems(ui.Center).Gap(components.Grid).Children(func() {
		components.IconTile(c, icon, tileBg, iconCol)
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(tint).SingleLine()
	})
}

// overviewCard draws one metric card: a coloured icon-and-title header with a
// chevron, the headline value and unit, detail lines, then the sparkline.
func overviewCard(c *ui.Context, cd card, series []float64) {
	t := c.Theme()
	tint := t.TextMuted
	if col, ok := components.MetricColor(cd.key); ok {
		tint = col
	}
	components.Surface(c, ui.Column(c).Width(components.CardWidth).Gap(components.Grid/2)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(components.Grid).Children(func() {
			if cd.icon != nil {
				components.IconTile(c, cd.icon, tint, ui.Hex("#ffffff"))
			}
			ui.Text(c, cd.title).FontSize(13).FontWeight(600).TextColor(tint).SingleLine().Grow(1)
			ui.Icon(c, icons.ChevronRight).FontSize(14).TextColor(t.TextMuted)
		})
		if cd.value != "" {
			spans := []ui.Span{{Text: cd.value, Size: 28, Weight: 600}}
			if cd.unit != "" && cd.value != format.Unknown {
				spans = append(spans, ui.Span{Text: " " + cd.unit, Size: 15, Weight: 500, Color: t.TextMuted})
			}
			ui.RichText(c, spans...).SingleLine()
		}
		for _, d := range cd.details {
			ui.Text(c, d).FontSize(12).TextColor(t.TextMuted).SingleLine()
		}
		if series != nil {
			ui.Spacer(c)
			ceiling := 0.0
			if cd.unit == "%" {
				ceiling = 100
			}
			components.Sparkline(c, series, ceiling, tint).Margin(components.Grid/2, 0, 0, 0)
		}
	})
}

// recent is the values of the points in the chart window before now, so
// cards and charts show the same span.
func recent(pts []hub.Point, now time.Time) []float64 {
	from := now.Add(-components.ChartWindow)
	var out []float64
	for _, p := range pts {
		if !p.T.Before(from) {
			out = append(out, p.V)
		}
	}
	return out
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

func ratio(used, total uint64) *float64 {
	if total == 0 {
		return nil
	}
	return ptr(float64(used) / float64(total) * 100)
}

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

func cards(s *model.Snapshot) []card {
	var out []card

	cpu := card{key: "cpu", icon: icons.CPU, title: "CPU", value: format.Unknown, unit: "%", series: "cpu"}
	load := format.Unknown
	if !partial(s, "cpu") {
		cpu.value = format.Number(ptr(s.CPU.Total))
		load = format.Load(s.CPU.Load)
	}
	cpu.details = []string{"Load " + load}
	out = append(out, cpu)

	mem := card{key: "memory", icon: icons.Memory, title: "Memory", value: format.Unknown, unit: "%", series: "mem"}
	memDetail := format.Unknown
	if !partial(s, "memory") {
		mem.value = format.Number(ratio(s.Memory.Used, s.Memory.Total))
		if s.Memory.Total > 0 {
			memDetail = format.Bytes(s.Memory.Used) + " / " + format.Bytes(s.Memory.Total)
		}
	}
	mem.details = []string{memDetail}
	out = append(out, mem)

	disk := card{key: "disk", icon: icons.Disk, title: "Disk", value: format.Unknown, unit: "%"}
	diskDetail := format.Unknown
	if !partial(s, "disks") && len(s.Disks) > 0 {
		var used, total uint64
		for _, d := range model.DistinctVolumes(s.Disks) {
			used += d.Used
			total += d.Total
		}
		disk.value = format.Number(ratio(used, total))
		if total > 0 {
			diskDetail = format.Bytes(used) + " / " + format.Bytes(total)
		}
	}
	disk.details = []string{diskDetail}
	out = append(out, disk)

	net := card{key: "network", icon: icons.Network, title: "Network", value: "", series: "net.rx"}
	rx, tx := format.Unknown, format.Unknown
	if !partial(s, "net") {
		var rxs, txs []*float64
		for _, i := range s.Net {
			if i.Name == "lo" || i.Name == "lo0" || !i.Up {
				continue
			}
			rxs, txs = append(rxs, i.RxBps), append(txs, i.TxBps)
		}
		rx, tx = format.Rate(sumRate(rxs)), format.Rate(sumRate(txs))
	}
	net.details = []string{"In " + rx, "Out " + tx}
	out = append(out, net)

	for i, g := range s.GPUs {
		name := g.Name
		if name == "" {
			name = fmt.Sprintf("GPU %d", i+1)
		}
		gpu := card{key: "gpu", icon: icons.GPU, title: name, value: format.Unknown, unit: "%", series: gpuSeries(i)}
		if !partial(s, "gpu") {
			gpu.value = format.Number(g.UtilPct)
			if g.MemUsed != nil && g.MemTotal != nil {
				gpu.details = append(gpu.details, format.Bytes(*g.MemUsed)+" / "+format.Bytes(*g.MemTotal))
			}
		}
		out = append(out, gpu)
	}

	if b := s.Battery; b != nil {
		bat := card{key: "battery", icon: icons.Energy, title: "Battery", value: format.Unknown, unit: "%"}
		if !partial(s, "battery") {
			bat.value = format.Number(ptr(b.ChargePct))
			state := "On battery"
			switch {
			case b.Charging:
				state = "Charging"
			case b.OnAC:
				state = "On AC"
			}
			bat.details = []string{state}
			if b.TimeRemaining != nil {
				bat.details = append(bat.details, format.Duration(*b.TimeRemaining)+" remaining")
			}
		}
		out = append(out, bat)
	}
	return out
}
