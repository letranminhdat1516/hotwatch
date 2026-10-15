// Package components holds the building blocks every page shares: cards,
// sparklines, time charts, state badges, banners and empty states. They
// take their colours from the frame's theme (ui.Theme), which the Shell
// sets for light and dark.
package components

import "github.com/egoist/mygo/ui"

// Layout metrics: an 8 DIP grid.
const (
	Grid       = 8
	CardRadius = 14
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
