//go:build !(darwin && arm64)

package symbols

import "github.com/egoist/mygo/ui"

// System has no symbols on this platform.
type System struct{}

// New returns a source without symbols.
func New() *System { return &System{} }

// Symbol is always nil here.
func (*System) Symbol(string, int, ui.Color) *ui.Bitmap { return nil }
