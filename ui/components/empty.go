package components

import "github.com/egoist/mygo/ui"

// EmptyState fills a page or a section that has nothing to show with a
// plain sentence saying so.
func EmptyState(c *ui.Context, text string) ui.Element {
	t := c.Theme()
	return ui.Column(c).Padding(6*Grid, 3*Grid).AlignItems(ui.Center).Radius(CardRadius).
		Border(1, t.Border).BorderStyle(ui.BorderDashed).Children(func() {
		ui.Text(c, text).FontSize(14).TextColor(t.TextMuted).TextAlign(ui.Center)
	})
}
