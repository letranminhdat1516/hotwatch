package components

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"hotwatch/ui/format"
)

// SegColors returns the colour drawn for each segment: a series colour for
// normal parts (in order), the neutral track for muted ones (free space).
// It matches StackedBar so a donut and a bar of the same data agree.
func SegColors(t *ui.Theme, segs []Segment) []ui.Color {
	palette := SeriesColors(t)
	colors := make([]ui.Color, len(segs))
	n := 0
	for i, s := range segs {
		if s.Muted {
			colors[i] = t.Border
			continue
		}
		colors[i] = palette[n%len(palette)]
		n++
	}
	return colors
}

// Donut draws segs as a ring sized by value, in the SegColors palette. size
// is the ring's width and height in DIPs. It draws nothing when every value
// is zero.
func Donut(c *ui.Context, segs []Segment, size float32) ui.Element {
	t := c.Theme()
	colors := SegColors(t, segs)
	values := make([]float64, len(segs))
	var total float64
	for i, s := range segs {
		values[i] = float64(s.Value)
		total += values[i]
	}
	box := ui.Box(c).Size(size, size).Shrink(0)
	if total == 0 {
		return box
	}
	return box.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		outer := min(r.W, r.H) / 2
		inner := outer * 0.62
		for i, s := range chart.Pie(values, 0.012) {
			p.FillPath(chart.Sector(cx, cy, inner, outer, s), colors[i])
		}
	})
}

// SegLegend lists each segment with its colour swatch, label and byte size.
func SegLegend(c *ui.Context, segs []Segment) ui.Element {
	t := c.Theme()
	colors := SegColors(t, segs)
	return ui.Column(c).Gap(Grid - 2).Grow(1).Children(func() {
		for i, s := range segs {
			ui.Row(c).Gap(Grid).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(10, 10).Radius(3).Background(colors[i])
				ui.Text(c, s.Label).FontSize(12).TextColor(t.TextMuted).Grow(1).NoWrap()
				ui.Text(c, format.Bytes(s.Value)).FontSize(12).FontWeight(600).NoWrap()
			})
		}
	})
}

// Ring is a single-value progress ring: frac (0–1) of the circle in color
// over a faint track of the same colour, with big and small centred inside
// as real text (so they read and test like any label).
func Ring(c *ui.Context, frac float64, size float32, color ui.Color, big, small string) ui.Element {
	t := c.Theme()
	frac = min(max(frac, 0), 1)
	return ui.Column(c).Size(size, size).Shrink(0).Center().Gap(2).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		outer := min(r.W, r.H) / 2
		inner := outer * 0.8
		parts := chart.Pie([]float64{frac, 1 - frac}, 0)
		if frac < 1 {
			p.FillPath(chart.Sector(cx, cy, inner, outer, parts[1]), color.Alpha(0.15))
		}
		if frac > 0 {
			p.FillPath(chart.Sector(cx, cy, inner, outer, parts[0]), color)
		}
	}).Children(func() {
		ui.Text(c, big).FontSize(22).FontWeight(700).TextColor(t.Text).SingleLine()
		if small != "" {
			ui.Text(c, small).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}
	})
}
