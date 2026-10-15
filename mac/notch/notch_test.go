package notch

import "testing"

// A 14-inch MacBook Pro in points: 1512 × 982, a 37 pt menu bar, the
// notch between x 662 and 850 (auxiliary areas end/start there).
var screen = Rect{X: 0, Y: 0, W: 1512, H: 982}

func TestNotchFromSafeAreaAndAuxiliaryAreas(t *testing.T) {
	n, ok := NotchRect(screen, 37, 662, 850)
	if !ok || n != (Rect{X: 662, Y: 0, W: 188, H: 37}) {
		t.Fatalf("NotchRect = %+v %v", n, ok)
	}
	// Without auxiliary areas the notch is centred at the fallback width.
	n, ok = NotchRect(screen, 37, 0, 0)
	if !ok || n.W != FallbackWidth || n.X != (1512-FallbackWidth)/2 {
		t.Fatalf("fallback = %+v %v", n, ok)
	}
	// No safe-area inset: no notch (an external display).
	if _, ok := NotchRect(screen, 0, 0, 0); ok {
		t.Fatal("a screen without a top inset has no notch")
	}
	// A screen that is not the primary keeps its own origin.
	n, _ = NotchRect(Rect{X: -1512, Y: 120, W: 1512, H: 982}, 37, -850, -662)
	if n != (Rect{X: -850, Y: 120, W: 188, H: 37}) {
		t.Fatalf("offset screen = %+v", n)
	}
}

func TestBarPanelAndHoverZones(t *testing.T) {
	n := Rect{X: 662, Y: 0, W: 188, H: 37}
	if ears := Ears(n, 64, 10); ears != (Rect{X: n.X - 74, Y: n.Y, W: n.W + 148, H: n.H}) {
		t.Fatalf("Ears = %+v", ears)
	}
	p := PanelRect(n, 560, 236)
	if p != (Rect{X: 476, Y: 0, W: 560, H: 236}) {
		t.Fatalf("PanelRect = %+v", p)
	}
	entry, stay := HoverZones(n, p)
	if !entry.Contains(756, 80) || entry.Contains(756, 120) || !entry.Contains(630, 10) || entry.Contains(600, 10) {
		t.Fatalf("entry zone %+v", entry)
	}
	if !stay.Contains(470, 250) || stay.Contains(440, 10) {
		t.Fatalf("stay zone %+v", stay)
	}
}

func TestFakeDetect(t *testing.T) {
	f := Fake{Geometry: Geometry{Screen: screen, Notch: Rect{X: 662, W: 188, H: 37}}, OK: true}
	g, ok := f.Detect()
	if !ok || g.Notch.W != 188 {
		t.Fatalf("Detect = %+v %v", g, ok)
	}
	var _ Detector = f
}
