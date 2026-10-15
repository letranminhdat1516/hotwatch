package components

import (
	"github.com/egoist/mygo/ui"

	"hotwatch/mac/symbols"
	"hotwatch/ui/icons"
)

// symbolSource draws SF Symbols on the Mac; nil leaves every icon on its
// Lucide drawing. Main thread only.
var symbolSource symbols.Source

// SetSymbolSource installs where Glyph gets SF Symbols.
func SetSymbolSource(s symbols.Source) { symbolSource = s }

// Glyph draws icon size DIPs square in color: its SF Symbol where the
// system has one (rendered at twice the size for Retina), else its Lucide
// drawing.
func Glyph(c *ui.Context, icon *ui.SVG, size float32, color ui.Color) ui.Element {
	if symbolSource != nil {
		if name := icons.Symbol(icon); name != "" {
			if bm := symbolSource.Symbol(name, int(size*2), color); bm != nil {
				return ui.Image(c, bm).Size(size, size).Shrink(0)
			}
		}
	}
	return ui.Icon(c, icon).FontSize(size).TextColor(color)
}
