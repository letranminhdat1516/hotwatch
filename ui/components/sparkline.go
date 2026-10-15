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
	return dotMatrix(c, pts, from, to, ceiling, tint, chartH)
}

// DotChart is a taller dot matrix with a time axis under it, for a page's
// main chart.
func DotChart(c *ui.Context, pts []hub.Point, from, to time.Time, ceiling float64, tint ui.Color, height float32) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(Grid / 2).Children(func() {
		dotMatrix(c, pts, from, to, ceiling, tint, height)
		ui.Box(c).Height(16).Draw(func(p *ui.Painter, r ui.Rect) {
			ticks := xTicksFor(to.Sub(from))
			x0, w := r.X+chartPad, r.W-2*chartPad
			for i, tk := range ticks {
				tw, _ := p.MeasureText(0, ui.Span{Text: tk.Label, Size: 11})
				x := x0 + w*tk.At - tw/2
				switch i {
				case 0:
					x = x0
				case len(ticks) - 1:
					x = x0 + w - tw
				}
				p.Text(x, r.Y, tk.Label, 11, t.TextMuted)
			}
		})
	})
}

// dotMatrix draws the dot matrix on its tinted backdrop at height.
func dotMatrix(c *ui.Context, pts []hub.Point, from, to time.Time, ceiling float64, tint ui.Color, height float32) ui.Element {
	box := ui.Box(c).Height(height).Radius(chartRadius).Background(tint.Alpha(0.10))
	if len(pts) == 0 || !to.After(from) {
		return box
	}
	off, on := tint.Alpha(0.18), tint
	return box.Draw(func(p *ui.Painter, r ui.Rect) {
		inner := ui.Rect{X: r.X + chartPad, Y: r.Y + chartPad, W: r.W - 2*chartPad, H: r.H - 2*chartPad}
		cols := int(inner.W / dotPitch)
		if cols < 1 {
			return
		}
		// Taller charts get more rows, so the dot pitch stays the same both ways.
		rows := max(dotRows, int(inner.H/dotPitch))
		lit := dotColumns(pts, from, to, cols, rows, ceiling)
		pitchX := inner.W / float32(cols)
		pitchY := inner.H / float32(rows)
		for j, n := range lit {
			cx := inner.X + (float32(j)+0.5)*pitchX
			for row := 0; row < rows; row++ {
				cy := inner.Y + inner.H - (float32(row)+0.5)*pitchY
				col := off
				if row < n {
					col = on
				}
				p.Fill(ui.Rect{X: cx - dotSize/2, Y: cy - dotSize/2, W: dotSize, H: dotSize}, col, dotSize/2)
			}
		}
	})
}

// dotColumns buckets pts into cols equal spans of [from, to] and returns,
// per column, how many of rows dots to light: round(avg/top*rows), where a
// bucket with a non-zero average lights at least one dot and a bucket
// without finite samples lights none. top is ceiling, or when ceiling is 0
// the largest bucket average plus 8% headroom (at least 1).
func dotColumns(pts []hub.Point, from, to time.Time, cols, rows int, ceiling float64) []int {
	sums := make([]float64, cols)
	counts := make([]int, cols)
	span := to.Sub(from).Seconds()
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
	avgs := make([]float64, cols)
	top := ceiling
	for i := range avgs {
		if counts[i] > 0 {
			avgs[i] = sums[i] / float64(counts[i])
			if ceiling == 0 {
				top = max(top, avgs[i])
			}
		}
	}
	if ceiling == 0 {
		top *= 1.08
	}
	if top <= 0 {
		top = 1
	}
	out := make([]int, cols)
	for i := range out {
		if counts[i] == 0 {
			continue
		}
		n := int(math.Round(min(max(avgs[i]/top, 0), 1) * float64(rows)))
		if n == 0 && avgs[i] > 0 {
			n = 1
		}
		out[i] = n
	}
	return out
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
