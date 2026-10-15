package components_test

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"

	hwui "hotwatch/ui"
	"hotwatch/ui/components"
)

func TestCardRadiusIsTheApprovedFourteen(t *testing.T) {
	if components.CardRadius != 14 {
		t.Fatalf("CardRadius = %v, want 14 (UI-DESIGN.md Tokens)", components.CardRadius)
	}
}

func TestSegmentPaletteStartsWithTheMemoryColour(t *testing.T) {
	segs := []components.Segment{{Label: "App", Value: 8}, {Label: "Wired", Value: 4}, {Label: "Compressed", Value: 7}, {Label: "Free", Value: 5, Muted: true}}
	for _, dark := range []bool{false, true} {
		th := hwui.Theme(dark)
		got := components.SegColors(th, segs)
		mem, _ := components.MetricColor("memory")
		if !dark && got[0] != mem {
			t.Errorf("light App segment %v, want the memory colour %v", got[0], mem)
		}
		if got[0] == th.Accent || got[1] == th.Accent || got[2] == th.Accent {
			t.Errorf("dark=%v: a memory segment uses the accent %v: %v", dark, th.Accent, got)
		}
		if got[3] != th.Border {
			t.Errorf("dark=%v: muted segment %v, want the track colour %v", dark, got[3], th.Border)
		}
		if got[0] == got[1] || got[1] == got[2] || got[0] == got[2] {
			t.Errorf("dark=%v: segment colours must differ: %v", dark, got)
		}
	}
}

func TestBarRowFillsWithItsTint(t *testing.T) {
	cpu, _ := components.MetricColor("cpu")
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(hwui.Theme(false))
		ui.Column(c).Padding(20).Background(c.Theme().Surface).Children(func() {
			components.BarRow(c, "Core 1", ptr(40), "40 %", cpu)
		})
	}, 600, 100)
	label, ok := tt.Find("Core 1")
	if !ok {
		t.Fatal("label not found")
	}
	// The bar starts after the 56 DIP label and the 8 DIP gap; sample well
	// inside the first 40 % of it.
	x, y := int(label.X+56+8+24), int(label.Y+label.H/2)
	r, g, b, _ := tt.Image().At(x, y).RGBA()
	got := ui.RGB(uint8(r>>8), uint8(g>>8), uint8(b>>8))
	if got != cpu {
		t.Fatalf("bar pixel at %d,%d is %v, want the cpu tint %v", x, y, got, cpu)
	}
}

// contrast is the WCAG 2 contrast ratio of a over b.
func contrast(a, b ui.Color) float64 {
	lum := func(c ui.Color) float64 {
		ch := func(v uint8) float64 {
			f := float64(v) / 255
			if f <= 0.03928 {
				return f / 12.92
			}
			return math.Pow((f+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	la, lb := lum(a)+0.05, lum(b)+0.05
	if la < lb {
		la, lb = lb, la
	}
	return la / lb
}

func TestTokenContrastMeetsAA(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := hwui.Theme(dark)
		cases := []struct {
			name string
			fg   ui.Color
			bg   ui.Color
		}{
			{"text on surface", th.Text, th.Surface},
			{"muted on surface", th.TextMuted, th.Surface},
			{"muted on background", th.TextMuted, th.Background},
			{"accent text on accent", th.AccentText, th.Accent},
			{"accent on surface", th.Accent, th.Surface},
		}
		for _, cs := range cases {
			if r := contrast(cs.fg, cs.bg); r < 4.5 {
				t.Errorf("dark=%v: %s contrast %.2f < 4.5", dark, cs.name, r)
			}
		}
	}
}

func ptr(v float64) *float64 { return &v }
