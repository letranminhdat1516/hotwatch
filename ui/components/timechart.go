package components

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"hotwatch/hub"
	"hotwatch/ui/format"
)

// ChartWindow is how far back a TimeChart looks.
const ChartWindow = 5 * time.Minute

// SeriesColors are the line colours: the accent, then the colours of the
// second and later series.
func SeriesColors(t *ui.Theme) []ui.Color {
	if t.Dark {
		return []ui.Color{t.Accent, ui.Hex("#8f9cf5"), ui.Hex("#f0b35a"), ui.Hex("#e486bd")}
	}
	return []ui.Color{t.Accent, ui.Hex("#5466d6"), ui.Hex("#c9821a"), ui.Hex("#bf4a8c")}
}

// FormatValue formats v in unit: "%" as "37.5 %", "B/s" and "B" with a
// binary unit ("2.0 KB/s"), anything else as "1.5 unit". Non-finite values
// are unknown.
func FormatValue(unit string, v float64) string {
	switch unit {
	case "%":
		return format.Percent(&v)
	case "B/s":
		return format.Rate(&v)
	case "B":
		if !finite(v) || v < 0 || v >= math.MaxUint64 {
			return format.Unknown
		}
		return format.Bytes(uint64(v))
	}
	n := format.Number(&v)
	if n == format.Unknown || unit == "" {
		return n
	}
	return n + " " + unit
}

func tickLabel(unit string, v float64) string {
	switch unit {
	case "%":
		return fmt.Sprintf("%.0f", v)
	case "B/s", "B":
		return FormatValue(unit, v)
	}
	return fmt.Sprintf("%g", v)
}

// latest is the newest finite value at or after from.
func latest(pts []hub.Point, from time.Time) (float64, bool) {
	for i := len(pts) - 1; i >= 0 && !pts[i].T.Before(from); i-- {
		if finite(pts[i].V) {
			return pts[i].V, true
		}
	}
	return 0, false
}

// TimeChart plots the last five minutes of each series against time, in a
// card: the title, a legend with each series' current value, the unit above
// the y axis and minutes along the x axis. "%" charts span 0–100; others
// span 0 to a round value above the largest point.
func TimeChart(c *ui.Context, title, unit string, series map[string][]hub.Point, now time.Time) ui.Element {
	t := c.Theme()
	from := now.Add(-ChartWindow)
	names := make([]string, 0, len(series))
	for n := range series {
		names = append(names, n)
	}
	slices.Sort(names)
	colors := SeriesColors(t)
	color := func(i int) ui.Color { return colors[i%len(colors)] }

	hi := 100.0
	if unit != "%" {
		hi = 0
		for _, n := range names {
			for _, p := range series[n] {
				if !p.T.Before(from) && finite(p.V) {
					hi = max(hi, p.V)
				}
			}
		}
		if hi <= 0 {
			hi = 1
		}
	}
	y, yTicks := yScale(unit, hi)
	at := func(v float64) float32 { return y.At(v) }
	var xTicks []chart.Tick
	for m := 5; m >= 0; m-- {
		label := fmt.Sprintf("−%dm", m)
		if m == 0 {
			label = "now"
		}
		xTicks = append(xTicks, chart.Tick{At: float32(5-m) / 5, Label: label})
	}
	axisLabel := func(side chart.Side, tick chart.Tick) {
		ui.Text(c, tick.Label).NoWrap().FontSize(11).TextColor(t.TextMuted)
	}

	return Surface(c, ui.Column(c).Gap(Grid+4)).Children(func() {
		ui.Row(c).Gap(2 * Grid).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, title).FontSize(14).FontWeight(600).NoWrap()
			ui.Spacer(c)
			for i, n := range names {
				val := format.Unknown
				if v, ok := latest(series[n], from); ok {
					val = FormatValue(unit, v)
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(10, 3).Radius(1.5).Background(color(i))
					ui.Text(c, n).FontSize(12).TextColor(t.TextMuted).NoWrap()
					ui.Text(c, val).FontSize(13).FontWeight(600).NoWrap()
				})
			}
		})
		ui.Column(c).Gap(Grid + 4).Children(func() {
			ui.Text(c, unit).FontSize(11).FontWeight(600).TextColor(t.TextMuted).NoWrap()
			chart.Plot(c, chart.Axes{Left: yTicks, Bottom: xTicks, Label: axisLabel}, func(area ui.Element) {
				area.Draw(func(p *ui.Painter, r ui.Rect) {
					for _, tick := range yTicks {
						p.Line(r.X, chart.Y(r, tick.At), r.X+r.W, chart.Y(r, tick.At), 1, t.Border)
					}
					for i, n := range names {
						xs, ys := plotPoints(series[n], from, at)
						if len(xs) == 0 {
							continue
						}
						if len(names) == 1 {
							p.FillPath(chart.Area(r, xs, nil, ys, chart.CurveMonotone), color(i).Alpha(0.12))
						}
						p.StrokePath(chart.Line(r, xs, ys, chart.CurveMonotone), 2, color(i))
					}
				})
			}).Height(20 * Grid).GapX(Grid).GapY(Grid / 2)
		})
	})
}

// plotPoints maps the points at or after from to plot fractions; a
// non-finite value breaks the line.
func plotPoints(pts []hub.Point, from time.Time, at func(float64) float32) (xs, ys []float32) {
	for _, p := range pts {
		if p.T.Before(from) {
			continue
		}
		xs = append(xs, float32(p.T.Sub(from).Seconds()/ChartWindow.Seconds()))
		v := float32(math.NaN())
		if finite(p.V) {
			v = min(max(at(p.V), 0), 1)
		}
		ys = append(ys, v)
	}
	return xs, ys
}

// yScale is the y axis for values up to hi. Byte scales step in powers of two
// (256 KB/s, 512 KB/s, 768 KB/s, 1 MB/s); others use round decimal steps.
func yScale(unit string, hi float64) (chart.Linear, []chart.Tick) {
	if (unit == "B/s" || unit == "B") && hi >= 1024 {
		step := 1024.0
		for step < hi/4 {
			step *= 2
		}
		n := int(math.Ceil(hi / step))
		top := float64(n) * step
		y := chart.NewLinear(0, top)
		ticks := make([]chart.Tick, n+1)
		for i := range ticks {
			v := float64(i) * step
			ticks[i] = chart.Tick{At: y.At(v), Label: tickLabel(unit, v)}
		}
		return y, ticks
	}
	y := chart.NewLinear(0, hi).Nice(4)
	y.Format = func(v float64) string { return tickLabel(unit, v) }
	return y, y.Ticks(4)
}
