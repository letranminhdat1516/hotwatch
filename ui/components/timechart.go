package components

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"hotwatch/hub"
	"hotwatch/ui/format"
)

// chartWindow is how far back charts look. The page header's 1m · 5m · 15m
// · 1h control sets it through the App; it is read and written on the main
// thread only, where every frame is built.
var chartWindow = 5 * time.Minute

// ChartWindow is how far back charts look.
func ChartWindow() time.Duration { return chartWindow }

// SetChartWindow changes how far back charts look; values that are not
// positive are ignored.
func SetChartWindow(d time.Duration) {
	if d > 0 {
		chartWindow = d
	}
}

// xTicksFor divides window into even steps labelled by their distance from
// the right edge ("−45s", "−3m", "−1h"), ending in "now": four steps, or
// five when the window is a multiple of five minutes under an hour.
func xTicksFor(window time.Duration) []chart.Tick {
	divisions := 4
	if window%(5*time.Minute) == 0 && window < time.Hour {
		divisions = 5
	}
	ticks := make([]chart.Tick, 0, divisions+1)
	for i := 0; i <= divisions; i++ {
		label := "now"
		if i < divisions {
			label = "−" + shortDuration(window-window*time.Duration(i)/time.Duration(divisions))
		}
		ticks = append(ticks, chart.Tick{At: float32(i) / float32(divisions), Label: label})
	}
	return ticks
}

// shortDuration is "2h", "15m", "45s" or "1m30s".
func shortDuration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

// SeriesColors are the colours of the parts of a donut or stacked bar, in
// order: the accent, then three more.
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

// latest is the newest finite value at or after from.
func latest(pts []hub.Point, from time.Time) (float64, bool) {
	for i := len(pts) - 1; i >= 0 && !pts[i].T.Before(from); i-- {
		if finite(pts[i].V) {
			return pts[i].V, true
		}
	}
	return 0, false
}

// Series is one line of a DotTimeChart: its legend name and its points.
type Series struct {
	Name string
	Pts  []hub.Point
}

// dotTimeChartH is the backdrop height of a page's main chart: 16 rows.
const dotTimeChartH = 16*dotPitch + 2*chartPad

// DotTimeChart is a page's main chart, in a card: the title in tint, the
// unit, a legend with each series' current value, the dot matrix of the
// series over the chart window (a second series in a lighter tint on the
// same scale) and the time axis. "%" charts span 0–100; others scale to
// the window's largest bucket. The legend keeps the given order.
func DotTimeChart(c *ui.Context, title, unit string, series []Series, now time.Time, tint ui.Color) ui.Element {
	t := c.Theme()
	window := ChartWindow()
	from := now.Add(-window)
	ceiling := 0.0
	if unit == "%" {
		ceiling = 100
	}
	colors := []ui.Color{tint, tint.Alpha(0.45)}
	layers := make([][]hub.Point, len(series))
	for i, s := range series {
		layers[i] = s.Pts
	}
	return Surface(c, ui.Column(c).Gap(Grid+4)).Children(func() {
		ui.Row(c).Gap(2 * Grid).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(tint).NoWrap()
			ui.Text(c, unit).FontSize(12).FontWeight(600).TextColor(t.TextMuted).NoWrap()
			ui.Spacer(c)
			for i, s := range series {
				val := format.Unknown
				if v, ok := latest(s.Pts, from); ok {
					val = FormatValue(unit, v)
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(10, 10).Radius(3).Background(colors[min(i, len(colors)-1)])
					ui.Text(c, s.Name).FontSize(12).TextColor(t.TextMuted).NoWrap()
					ui.Text(c, val).FontSize(13).FontWeight(600).NoWrap()
				})
			}
		})
		dotMatrix(c, layers, from, now, ceiling, tint, dotTimeChartH)
		timeAxis(c, window)
	})
}
