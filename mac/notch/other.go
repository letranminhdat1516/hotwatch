//go:build !(darwin && arm64)

package notch

// System finds no notch on this platform; the menu bar item stands in.
type System struct{}

// Detect reports no notch.
func (System) Detect() (Geometry, bool) { return Geometry{}, false }
