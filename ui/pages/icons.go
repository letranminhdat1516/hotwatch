package pages

import (
	"github.com/egoist/mygo/ui"

	"hotwatch/mac/appicon"
	"hotwatch/model"
	"hotwatch/ui/components"
)

// iconSource hands out application icons to every page. Pages are
// functions, so it is package state; it is read and set on the main thread
// only. nil: initials tiles everywhere.
var iconSource appicon.Source

// SetIconSource installs the application icon source (main thread).
func SetIconSource(src appicon.Source) { iconSource = src }

// appIcon draws the icon tile of the application at bundle, or the initials
// of name while the icon loads or when the host has none. A helper nested
// in an app (Google Chrome Helper (Renderer).app inside Google Chrome.app)
// shows its app's icon: the helper's own is a blank document.
func appIcon(c *ui.Context, name, bundle string) ui.Element {
	return userIcon(c, name, bundle, "")
}

// procIcon is appIcon for a process: one without an app shows the app of
// its name, or the system's gear when it is a system process.
func procIcon(c *ui.Context, p *model.Process) ui.Element {
	return userIcon(c, p.Name, p.AppBundle, p.User)
}

func userIcon(c *ui.Context, name, bundle, user string) ui.Element {
	var bm *ui.Bitmap
	if iconSource != nil {
		bm, _ = iconSource.Icon(appicon.ForProcess(name, bundle, user))
	}
	return components.AppIcon(c, name, bm)
}

// appIconSized is appIcon at size DIPs, for an inspector's header.
func appIconSized(c *ui.Context, name, bundle string, size float32) ui.Element {
	var bm *ui.Bitmap
	if iconSource != nil {
		bm, _ = iconSource.Icon(appicon.ForProcess(name, bundle, ""))
	}
	return components.AppIconSized(c, name, bm, size)
}
