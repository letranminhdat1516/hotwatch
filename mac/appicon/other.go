//go:build !(darwin && arm64)

package appicon

import "github.com/egoist/mygo/ui"

// Darwin is the icon source on every other platform: it never answers, so
// applications keep their initials tiles.
type Darwin struct{}

// NewDarwin returns a source that never answers.
func NewDarwin(post func(func()), px int) *Darwin { return &Darwin{} }

// Icon never answers.
func (*Darwin) Icon(string) (*ui.Bitmap, bool) { return nil, false }
