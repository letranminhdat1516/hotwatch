//go:build darwin && arm64

package symbols

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRendersATintedSystemSymbol(t *testing.T) {
	blue := ui.Hex("#0a84ff")
	img := render("cpu", 48, blue)
	if img == nil {
		t.Fatal("cpu symbol not rendered")
	}
	b := img.Bounds()
	if b.Dx() != 48 || b.Dy() != 48 {
		t.Fatalf("size %v", b)
	}
	solid := 0
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a>>8 == 255 {
				solid++
				if uint8(r>>8) != blue.R || uint8(g>>8) != blue.G || uint8(bl>>8) != blue.B {
					t.Fatalf("pixel %d,%d = %d,%d,%d, want the tint", x, y, r>>8, g>>8, bl>>8)
				}
			}
		}
	}
	if solid < 100 {
		t.Fatalf("only %d solid pixels: the symbol is empty", solid)
	}
	if render("no.such.symbol.name", 48, blue) != nil {
		t.Fatal("an unknown symbol rendered")
	}
}

func TestSourceCachesByNameSizeAndColour(t *testing.T) {
	s := New()
	a := s.Symbol("memorychip", 24, ui.Hex("#30d158"))
	b := s.Symbol("memorychip", 24, ui.Hex("#30d158"))
	if a == nil || a != b {
		t.Fatalf("not cached: %p %p", a, b)
	}
	if c := s.Symbol("memorychip", 24, ui.Hex("#ffffff")); c == a {
		t.Fatal("a different colour reused the bitmap")
	}
}
