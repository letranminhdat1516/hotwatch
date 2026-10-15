// Package appicon hands application icons to the UI without ever blocking
// a frame. The darwin implementation asks AppKit on a worker and delivers
// through the main-thread hop it was built with; everywhere else, and in
// tests, a Source that never answers leaves the UI on its initials tiles.
package appicon

import "github.com/egoist/mygo/ui"

// Source answers Icon from a cache: the bitmap and true when it has one,
// nil and false otherwise. A miss schedules a load whose completion the
// implementation delivers on the main thread, after which the caller
// re-renders and gets the bitmap. Icon is called on the main thread only.
type Source interface {
	Icon(bundlePath string) (*ui.Bitmap, bool)
}

// None is the Source for hosts and platforms without icons: every bundle
// shows its initials.
type None struct{}

// Icon never answers.
func (None) Icon(string) (*ui.Bitmap, bool) { return nil, false }
