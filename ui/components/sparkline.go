package components

import (
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// Sparkline draws values, oldest first, as a line in the accent colour over
// a faint fill. ceiling is the top of the scale; 0 scales to the largest
// value. Non-finite values break the line; fewer than two values draw
// nothing but keep the room.
func Sparkline(c *ui.Context, values []float64, ceiling float64) ui.Element {
	box := ui.Box(c).Height(5 * Grid)
	if len(values) < 2 {
		return box
	}
	top := ceiling
	if top == 0 {
		for _, v := range values {
			if finite(v) {
				top = max(top, v)
			}
		}
	}
	if top <= 0 {
		top = 1
	}
	xs := make([]float32, len(values))
	ys := make([]float32, len(values))
	for i, v := range values {
		xs[i] = float32(i) / float32(len(values)-1)
		ys[i] = float32(math.NaN())
		if finite(v) {
			ys[i] = float32(min(max(v/top, 0), 1))
		}
	}
	accent := c.Theme().Accent
	return box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.FillPath(chart.Area(r, xs, nil, ys, chart.CurveMonotone), accent.Alpha(0.12))
		p.StrokePath(chart.Line(r, xs, ys, chart.CurveMonotone), 1.5, accent)
	})
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
