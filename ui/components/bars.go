package components

import (
	"github.com/egoist/mygo/ui"

	"hotwatch/ui/format"
)

const (
	barHeight  = 8
	labelWidth = 7 * Grid
	valueWidth = 8 * Grid
)

// BarRow shows one labelled level: the label, a bar filled to pct (0–100)
// and value text. A nil pct is unknown: an empty bar. Fills turn amber from
// 75 and red from 90.
func BarRow(c *ui.Context, label string, pct *float64, value string) ui.Element {
	t := c.Theme()
	return ui.Row(c).Gap(Grid).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).Width(labelWidth).SingleLine()
		track := ui.Box(c).Grow(1).Height(barHeight).Radius(barHeight / 2).Background(t.Border)
		if pct != nil && finite(*pct) {
			fill := min(max(*pct/100, 0), 1)
			col := t.Accent
			switch {
			case *pct >= 90:
				col = t.Danger
			case *pct >= 75:
				col = t.Warning
			}
			track.Draw(func(p *ui.Painter, r ui.Rect) {
				if fill > 0 {
					p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*float32(fill), barHeight), H: r.H}, col, barHeight/2)
				}
			})
		}
		ui.Text(c, value).FontSize(12).FontWeight(600).Width(valueWidth).TextAlign(ui.End).SingleLine()
	})
}

// Segment is one part of a StackedBar. Muted segments (free space) are
// drawn in the neutral track colour instead of a series colour.
type Segment struct {
	Label string
	Value uint64
	Muted bool
}

// StackedBar draws segments side by side, sized by their values, with a
// legend of each label and its size beneath.
func StackedBar(c *ui.Context, segs []Segment) ui.Element {
	t := c.Theme()
	palette := SeriesColors(t)
	colors := make([]ui.Color, len(segs))
	var total uint64
	n := 0
	for i, s := range segs {
		total += s.Value
		if s.Muted {
			colors[i] = t.Border
			continue
		}
		colors[i] = palette[n%len(palette)]
		n++
	}
	return ui.Column(c).Gap(Grid + 4).Children(func() {
		ui.Box(c).Height(2 * Grid).Radius(Grid).Background(t.Border).Draw(func(p *ui.Painter, r ui.Rect) {
			if total == 0 {
				return
			}
			x := r.X
			for i, s := range segs {
				w := r.W * float32(float64(s.Value)/float64(total))
				if w > 0 {
					p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, colors[i], 0)
				}
				x += w
			}
		})
		ui.Row(c).Gap(3 * Grid).Wrap().Children(func() {
			for i, s := range segs {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(10, 10).Radius(2).Background(colors[i])
					ui.Text(c, s.Label).FontSize(12).TextColor(t.TextMuted).NoWrap()
					ui.Text(c, format.Bytes(s.Value)).FontSize(13).FontWeight(600).NoWrap()
				})
			}
		})
	})
}
