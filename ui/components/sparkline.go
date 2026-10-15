package components

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Sparkline draws values, oldest first, as a grid of dots: each column is a
// time bucket whose dots fill from the bottom up to the value, in tint;
// unfilled dots stay faint. ceiling is the top of the scale; 0 scales to the
// largest value. Unknown values leave a column faint; fewer than two values
// draw nothing but keep the room.
func Sparkline(c *ui.Context, values []float64, ceiling float64, tint ui.Color) ui.Element {
	box := ui.Box(c).Height(6 * Grid)
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
	top *= 1.08 // headroom so the peak never clips the top edge
	ys := make([]float32, len(values))
	for i, v := range values {
		ys[i] = float32(math.NaN())
		if finite(v) {
			ys[i] = float32(min(max(v/top, 0), 1))
		}
	}
	empty := tint.Alpha(0.12)
	return box.Draw(func(p *ui.Painter, r ui.Rect) {
		const rows = 7
		cols := len(ys)
		if maxCols := int(r.W / 6); cols > maxCols {
			cols = maxCols
		}
		if cols < 1 {
			return
		}
		pitchX := r.W / float32(cols)
		pitchY := r.H / float32(rows)
		d := min(pitchX, pitchY) * 0.46
		for j := 0; j < cols; j++ {
			idx := 0
			if cols > 1 {
				idx = j * (len(ys) - 1) / (cols - 1)
			}
			y := ys[idx]
			filled := -1 // unknown: all dots faint
			if !math.IsNaN(float64(y)) {
				filled = int(float64(y)*rows + 0.5)
			}
			cx := r.X + (float32(j)+0.5)*pitchX
			for row := 0; row < rows; row++ {
				cy := r.Y + r.H - (float32(row)+0.5)*pitchY
				col := empty
				if filled >= 0 && row < filled {
					col = tint
				}
				p.Fill(ui.Rect{X: cx - d/2, Y: cy - d/2, W: d, H: d}, col, d/2)
			}
		}
	})
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
