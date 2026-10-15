// Package symbols draws Apple's SF Symbols for the interface: macOS renders
// each one by name at run time (nothing is bundled), tinted with the
// caller's colour and cached. Where the system has no symbols (other
// platforms, tests on Linux), Symbol returns nil and callers draw their own
// icon instead.
package symbols

import "github.com/egoist/mygo/ui"

// Source hands out tinted symbol bitmaps. Main thread only.
type Source interface {
	// Symbol is name drawn px pixels square in color; nil when the system
	// has no such symbol.
	Symbol(name string, px int, color ui.Color) *ui.Bitmap
}
