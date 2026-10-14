// Package components holds the building blocks every page shares: cards,
// sparklines, time charts, state badges, banners and empty states. They
// take their colours from the frame's theme (ui.Theme), which the Shell
// sets for light and dark.
package components

import (
	"github.com/egoist/mygo/ui"

	"hotwatch/ui/format"
)

// Layout metrics: an 8 DIP grid.
const (
	Grid       = 8
	CardRadius = 12
	CardWidth  = 33 * Grid
)

// Surface styles e as a card: the theme's surface, a hairline border, the
// card radius and padding.
func Surface(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	e.Padding(2*Grid).Radius(CardRadius).Background(t.Surface).Border(1, t.Border)
	if !t.Dark {
		e.Shadow(0, 1, 2, 0, ui.RGBA(16, 24, 40, 0.05))
	}
	return e
}

// Card shows one metric: a small muted title, the value large with its unit
// beside it, detail lines, and a sparkline of series. An unknown value
// (format.Unknown) is shown without its unit. A "%" unit scales the
// sparkline from 0 to 100; others scale it to the data.
func Card(c *ui.Context, title, value, unit string, details []string, series []float64) ui.Element {
	t := c.Theme()
	return Surface(c, ui.Column(c).Width(CardWidth).Gap(Grid/2)).Children(func() {
		ui.Text(c, title).FontSize(12).FontWeight(500).TextColor(t.TextMuted).SingleLine()
		if value != "" {
			spans := []ui.Span{{Text: value, Size: 28, Weight: 600}}
			if unit != "" && value != format.Unknown {
				spans = append(spans, ui.Span{Text: " " + unit, Size: 15, Weight: 500, Color: t.TextMuted})
			}
			ui.RichText(c, spans...).SingleLine()
		}
		for _, d := range details {
			ui.Text(c, d).FontSize(12).TextColor(t.TextMuted).SingleLine()
		}
		if series != nil {
			ui.Spacer(c) // sparklines line up along the bottom of a row
			ceiling := 0.0
			if unit == "%" {
				ceiling = 100
			}
			Sparkline(c, series, ceiling).Margin(Grid/2, 0, 0, 0)
		}
	})
}
