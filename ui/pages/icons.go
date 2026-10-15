package pages

import (
	"github.com/egoist/mygo/ui"

	"hotwatch/mac/appicon"
	"hotwatch/ui/components"
)

// iconSource hands out application icons to every page. Pages are
// functions, so it is package state; it is read and set on the main thread
// only. nil: initials tiles everywhere.
var iconSource appicon.Source

// SetIconSource installs the application icon source (main thread).
func SetIconSource(src appicon.Source) { iconSource = src }

// appIcon draws the icon tile of the application at bundle, or the initials
// of name while the icon loads or when the host has none.
func appIcon(c *ui.Context, name, bundle string) ui.Element {
	var bm *ui.Bitmap
	if iconSource != nil && bundle != "" {
		bm, _ = iconSource.Icon(bundle)
	}
	return components.AppIcon(c, name, bm)
}
