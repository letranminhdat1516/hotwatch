// Package appicon hands application icons to the UI without ever blocking
// a frame. The darwin implementation asks AppKit on a worker and delivers
// through the main-thread hop it was built with; everywhere else, and in
// tests, a Source that never answers leaves the UI on its initials tiles.
package appicon

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

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

// BundlePath reports whether p is an application bundle path the source
// may hand to AppKit: absolute, ending in ".app", without "..", under 1 KB.
// A process list is untrusted input (a remote agent sends its own), so
// anything else is refused before it reaches a native API.
func BundlePath(p string) bool {
	return len(p) < 1024 && strings.HasPrefix(p, "/") && strings.HasSuffix(p, ".app") &&
		!strings.Contains(p, "/../") && !strings.Contains(p, "\x00")
}

// Executable asks for the system's icon of a Unix executable, the one
// Finder shows for command-line programs: processes without an app bundle
// (daemons, tools) get it instead of initials.
const Executable = "public.unix-executable"

// Key reports whether k is something a Source may be asked for: a bundle
// path (BundlePath) or Executable.
func Key(k string) bool { return k == Executable || BundlePath(k) }
