package components

import (
	"unicode"

	"github.com/egoist/mygo/ui"
)

// AppIconSize is the side of an application icon tile in lists.
const AppIconSize = 20

// AppIcon is a 20 DIP rounded tile for an application: its icon when bm is
// set, otherwise its initials on a tint derived from the name, so a list
// never shows an empty cell while icons load or on hosts without them.
func AppIcon(c *ui.Context, name string, bm *ui.Bitmap) ui.Element {
	if bm != nil {
		return ui.Image(c, bm).Size(AppIconSize, AppIconSize).Radius(5).Clip().Shrink(0).Label(name + " icon")
	}
	return ui.Row(c).Size(AppIconSize, AppIconSize).Radius(5).Shrink(0).Center().
		Background(nameTint(name)).Children(func() {
		ui.Text(c, Initials(name)).FontSize(9).FontWeight(700).TextColor(ui.RGB(255, 255, 255)).SingleLine()
	})
}

// Initials are the first letters of the first and last words of name,
// upper-cased: "Google Chrome" → "GC", "node" → "N", "" → "?".
func Initials(name string) string {
	var first, last rune
	inWord := false
	for _, r := range name {
		letter := unicode.IsLetter(r) || unicode.IsDigit(r)
		if letter && !inWord {
			if first == 0 {
				first = unicode.ToUpper(r)
			} else {
				last = unicode.ToUpper(r)
			}
		}
		inWord = letter
	}
	switch {
	case first == 0:
		return "?"
	case last == 0:
		return string(first)
	}
	return string(first) + string(last)
}

// nameTint is a muted colour chosen by the FNV-1a hash of name, so the same
// application always gets the same tile.
func nameTint(name string) ui.Color {
	h := uint32(2166136261)
	for i := 0; i < len(name); i++ {
		h = (h ^ uint32(name[i])) * 16777619
	}
	palette := []ui.Color{ui.Hex("#6366f1"), ui.Hex("#0ea5e9"), ui.Hex("#10b981"), ui.Hex("#f59e0b"),
		ui.Hex("#f97316"), ui.Hex("#14b8a6"), ui.Hex("#8b5cf6"), ui.Hex("#64748b")}
	return palette[h%uint32(len(palette))]
}

// AppIconSized is AppIcon at size DIPs (an inspector's header).
func AppIconSized(c *ui.Context, name string, bm *ui.Bitmap, size float32) ui.Element {
	radius := size * 0.22
	if bm != nil {
		return ui.Image(c, bm).Size(size, size).Radius(radius).Clip().Shrink(0).Label(name + " icon")
	}
	return ui.Row(c).Size(size, size).Radius(radius).Shrink(0).Center().
		Background(nameTint(name)).Children(func() {
		ui.Text(c, Initials(name)).FontSize(size*0.4).FontWeight(700).TextColor(ui.RGB(255, 255, 255)).SingleLine()
	})
}
