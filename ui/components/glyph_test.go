package components_test

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	"hotwatch/ui/components"
	"hotwatch/ui/icons"
)

type fakeSymbols struct{ asked []string }

func (f *fakeSymbols) Symbol(name string, px int, col ui.Color) *ui.Bitmap {
	f.asked = append(f.asked, name)
	if name == "cpu.fill" {
		return ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, px, px)))
	}
	return nil
}

func TestGlyphDrawsTheSystemSymbolWhenThereIsOne(t *testing.T) {
	f := &fakeSymbols{}
	components.SetSymbolSource(f)
	defer components.SetSymbolSource(nil)
	tt := ui.NewTester(func(c *ui.Context) {
		components.Glyph(c, icons.CPU, 16, ui.Hex("#0a84ff")).Label("cpu glyph")
		components.Glyph(c, icons.Activity, 16, ui.Hex("#0a84ff")).Label("activity glyph") // fake has none
	}, 200, 100)
	r, ok := tt.Find("cpu glyph")
	if !ok || r.W != 16 || r.H != 16 {
		t.Fatalf("cpu glyph %v %v", r, ok)
	}
	if len(f.asked) < 2 || f.asked[0] != "cpu.fill" || f.asked[1] != "waveform.path.ecg" {
		t.Fatalf("asked %v", f.asked)
	}
	if _, ok := tt.Find("activity glyph"); !ok {
		t.Fatal("the Lucide fallback was not drawn")
	}
}
