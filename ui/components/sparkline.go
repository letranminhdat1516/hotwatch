package components

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
)

// Dot-matrix chart metrics (UI-DESIGN.md "Dot-matrix chart").
const (
	dotRows     = 8
	dotPitch    = 6   // column pitch, DIPs
	dotSize     = 3.4 // dot diameter, DIPs
	chartH      = 72  // backdrop height
	chartPad    = 10  // backdrop padding around the matrix
	chartRadius = 10
)

// Sparkline draws pts over the window [from, to] as a dot matrix on a
// backdrop tinted with tint: each column is one time bucket, lit from the
// bottom up to the bucket's average; a bucket without samples stays off, so
// a gap in the data shows as a gap. ceiling is the top of the scale; 0
// scales to the window's largest bucket with headroom. Unknown values
// (NaN, Inf) are ignored.
func Sparkline(c *ui.Context, pts []hub.Point, from, to time.Time, ceiling float64, tint ui.Color) ui.Element {
	return dotMatrix(c, [][]hub.Point{pts}, from, to, ceiling, tint, chartH)
}

// MiniDots is a dot matrix of height for small tiles, such as the nook's.
func MiniDots(c *ui.Context, pts []hub.Point, from, to time.Time, ceiling float64, tint ui.Color, height float32) ui.Element {
	return dotMatrix(c, [][]hub.Point{pts}, from, to, ceiling, tint, height)
}

// DotChart is a taller dot matrix with a time axis under it, for a page's
// main chart.
func DotChart(c *ui.Context, pts []hub.Point, from, to time.Time, ceiling float64, tint ui.Color, height float32) ui.Element {
	return ui.Column(c).Gap(Grid / 2).Children(func() {
		dotMatrix(c, [][]hub.Point{pts}, from, to, ceiling, tint, height)
		timeAxis(c, to.Sub(from))
	})
}

// timeAxis is the row of tick labels under a dot matrix, "−5m" … "now",
// aligned with the matrix's padding.
func timeAxis(c *ui.Context, window time.Duration) ui.Element {
	t := c.Theme()
	return ui.Row(c).Justify(ui.SpaceBetween).Padding(0, chartPad).Children(func() {
		for _, tk := range xTicksFor(window) {
			ui.Text(c, tk.Label).FontSize(11).TextColor(t.TextMuted).NoWrap()
		}
	})
}

// DotLayer is one series of a StackedDotChart and the colour of its dots.
type DotLayer struct {
	Pts   []hub.Point
	Color ui.Color
}

// StackedDotChart draws layers on one scale, front first: each lights its
// dots from the bottom in its colour, and a later layer shows where it
// rises above the earlier ones. For a split such as CPU time, pass the
// lower part first and the total second (System, then User + System). The
// backdrop is tinted with tint; a time axis follows.
func StackedDotChart(c *ui.Context, layers []DotLayer, from, to time.Time, ceiling float64, tint ui.Color, height float32) ui.Element {
	pts := make([][]hub.Point, len(layers))
	colors := make([]ui.Color, len(layers))
	for i, l := range layers {
		pts[i], colors[i] = l.Pts, l.Color
	}
	return ui.Column(c).Gap(Grid / 2).Children(func() {
		dotMatrixColored(c, pts, colors, from, to, ceiling, tint, height)
		timeAxis(c, to.Sub(from))
	})
}

// dotMatrix draws layers on a tinted backdrop at height: the first layer in
// tint, the second in a lighter tint behind it (it shows where it rises
// above the first), all on one scale.
func dotMatrix(c *ui.Context, layers [][]hub.Point, from, to time.Time, ceiling float64, tint ui.Color, height float32) ui.Element {
	return dotMatrixColored(c, layers, []ui.Color{tint, tint.Alpha(0.45)}, from, to, ceiling, tint, height)
}

// dotMatrixColored draws layers in colors (the last colour repeats).
func dotMatrixColored(c *ui.Context, layers [][]hub.Point, colors []ui.Color, from, to time.Time, ceiling float64, tint ui.Color, height float32) ui.Element {
	box := ui.Box(c).Height(height).Radius(chartRadius).Background(tint.Alpha(0.10))
	empty := true
	for _, l := range layers {
		empty = empty && len(l) == 0
	}
	if empty || !to.After(from) {
		return box
	}
	off := tint.Alpha(0.18)
	return box.Draw(func(p *ui.Painter, r ui.Rect) {
		inner := ui.Rect{X: r.X + chartPad, Y: r.Y + chartPad, W: r.W - 2*chartPad, H: r.H - 2*chartPad}
		cols := int(inner.W / dotPitch)
		if cols < 1 {
			return
		}
		// Taller charts get more rows, so the dot pitch stays the same both ways.
		rows := max(dotRows, int(inner.H/dotPitch))
		lit := DotLayers(layers, from, to, cols, rows, ceiling)
		pitchX := inner.W / float32(cols)
		pitchY := inner.H / float32(rows)
		for j := 0; j < cols; j++ {
			cx := inner.X + (float32(j)+0.5)*pitchX
			for row := 0; row < rows; row++ {
				cy := inner.Y + inner.H - (float32(row)+0.5)*pitchY
				col := off
				for l := len(lit) - 1; l >= 0; l-- { // the first layer paints last, on top
					if row < lit[l][j] {
						col = colors[min(l, len(colors)-1)]
					}
				}
				p.Fill(ui.Rect{X: cx - dotSize/2, Y: cy - dotSize/2, W: dotSize, H: dotSize}, col, dotSize/2)
			}
		}
	})
}

// dotColumns buckets pts into cols equal spans of [from, to] and returns,
// per column, how many of rows dots to light.
func dotColumns(pts []hub.Point, from, to time.Time, cols, rows int, ceiling float64) []int {
	return DotLayers([][]hub.Point{pts}, from, to, cols, rows, ceiling)[0]
}

// DotLayers buckets each layer into cols equal spans of [from, to] and
// returns, per layer and column, how many of rows dots to light:
// round(avg/top*rows), where a bucket with a non-zero average lights at
// least one dot and a bucket without finite samples lights none. top is
// ceiling, or when ceiling is 0 the largest bucket average of any layer plus
// 8% headroom (at least 1), so the layers share one scale.
func DotLayers(layers [][]hub.Point, from, to time.Time, cols, rows int, ceiling float64) [][]int {
	span := to.Sub(from).Seconds()
	avgs := make([][]float64, len(layers))
	has := make([][]bool, len(layers))
	top := ceiling
	for l, pts := range layers {
		sums := make([]float64, cols)
		counts := make([]int, cols)
		for _, pt := range pts {
			if !finite(pt.V) || pt.T.Before(from) || pt.T.After(to) {
				continue
			}
			i := int(pt.T.Sub(from).Seconds() / span * float64(cols))
			if i >= cols {
				i = cols - 1
			}
			sums[i] += pt.V
			counts[i]++
		}
		avgs[l] = make([]float64, cols)
		has[l] = make([]bool, cols)
		for i := range sums {
			if counts[i] > 0 {
				avgs[l][i] = sums[i] / float64(counts[i])
				has[l][i] = true
				if ceiling == 0 {
					top = max(top, avgs[l][i])
				}
			}
		}
	}
	if ceiling == 0 {
		top *= 1.08
	}
	if top <= 0 {
		top = 1
	}
	out := make([][]int, len(layers))
	for l := range layers {
		out[l] = make([]int, cols)
		for i := range out[l] {
			if !has[l][i] {
				continue
			}
			n := int(math.Round(min(max(avgs[l][i]/top, 0), 1) * float64(rows)))
			if n == 0 && avgs[l][i] > 0 {
				n = 1
			}
			out[l][i] = n
		}
	}
	return out
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
