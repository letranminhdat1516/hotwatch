package components

import (
	"github.com/egoist/mygo/ui"

	"hotwatch/ui/icons"
)

// BannerKind picks a banner's colour and icon.
type BannerKind int

const (
	BannerWarning BannerKind = iota // amber: stale, verify host key
	BannerError                     // red: offline, rejected, host key changed
)

// Banner shows a one-line notice about the host under the page title.
func Banner(c *ui.Context, kind BannerKind, text string) ui.Element {
	t := c.Theme()
	col, icon := t.Warning, icons.Warning
	if kind == BannerError {
		col, icon = t.Danger, icons.Offline
	}
	return ui.Row(c).Gap(Grid+2).Padding(Grid+2, 2*Grid).Radius(Grid).
		Background(col.Alpha(0.10)).Border(1, col.Alpha(0.35)).Children(func() {
		Glyph(c, icon, 16, col)
		ui.Text(c, text).FontWeight(500)
	})
}
