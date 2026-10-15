// Package notch finds the MacBook's camera notch and lays out the nook
// around it: the idle bar under its bottom edge, the island that grows out
// of it, and the zones where the pointer opens and keeps the island open.
// Every rectangle is in global screen DIPs with the origin at the top-left
// corner of the primary display, as mygo's window bounds are.
package notch

// Rect is a rectangle in global screen DIPs, top-left origin.
type Rect struct{ X, Y, W, H float64 }

// Contains reports whether the point x, y lies inside r.
func (r Rect) Contains(x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Inset grows r by dx on the left and right and dy on the top and bottom
// (negative values shrink it).
func (r Rect) Inset(dx, dy float64) Rect {
	return Rect{X: r.X - dx, Y: r.Y - dy, W: r.W + 2*dx, H: r.H + 2*dy}
}

// Geometry is the notched screen and its notch.
type Geometry struct {
	Screen Rect
	Notch  Rect
}

// Detector finds the notch; ok is false when no active display has one.
type Detector interface {
	Detect() (g Geometry, ok bool)
}

// FallbackWidth is the notch width used when the screen does not report
// its menu-bar areas: about the 14- and 16-inch MacBook Pro's.
const FallbackWidth = 200

// NotchRect is the notch of screen given its safe-area top inset (the menu
// bar height on a notched display, 0 elsewhere) and, when known, the
// right edge of the menu-bar area left of the notch and the left edge of
// the one right of it (both 0 when unknown).
func NotchRect(screen Rect, insetTop, leftMaxX, rightMinX float64) (Rect, bool) {
	if insetTop <= 0 {
		return Rect{}, false
	}
	if rightMinX > leftMaxX {
		return Rect{X: leftMaxX, Y: screen.Y, W: rightMinX - leftMaxX, H: insetTop}, true
	}
	return Rect{X: screen.X + (screen.W-FallbackWidth)/2, Y: screen.Y, W: FallbackWidth, H: insetTop}, true
}

// Ears is the idle nook: the notch widened by ear DIPs on each side, as
// tall as the notch, plus shoulder DIPs each side for the flared top
// corners that blend it into the screen's edge.
func Ears(n Rect, ear, shoulder float64) Rect {
	return Rect{X: n.X - ear - shoulder, Y: n.Y, W: n.W + 2*(ear+shoulder), H: n.H}
}

// PanelRect is the island: w × h, centred on the notch, its top at the
// top of the screen so it grows out of the notch.
func PanelRect(n Rect, w, h float64) Rect {
	return Rect{X: n.X + (n.W-w)/2, Y: n.Y, W: w, H: h}
}

// HoverZones are where the pointer opens the island (the notch, 40 DIPs
// wider on each side and 60 deeper) and where it keeps it open (the
// island, 20 DIPs around).
func HoverZones(n, panel Rect) (entry, stay Rect) {
	entry = Rect{X: n.X - 40, Y: n.Y, W: n.W + 80, H: n.H + 60}
	return entry, panel.Inset(20, 20)
}

// Fake is a Detector answering Geometry and OK.
type Fake struct {
	Geometry Geometry
	OK       bool
}

// Detect answers the fake's geometry.
func (f Fake) Detect() (Geometry, bool) { return f.Geometry, f.OK }
