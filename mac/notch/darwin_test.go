//go:build darwin && arm64

package notch

import "testing"

func TestDetectOnThisMac(t *testing.T) {
	g, ok := System{}.Detect()
	if !ok {
		t.Skip("no notched display active (lid closed or external screen only)")
	}
	if g.Notch.W < 120 || g.Notch.W > 300 || g.Notch.H < 24 || g.Notch.H > 50 {
		t.Fatalf("implausible notch %+v on screen %+v", g.Notch, g.Screen)
	}
	mid := g.Notch.X + g.Notch.W/2
	if d := mid - (g.Screen.X + g.Screen.W/2); d < -2 || d > 2 {
		t.Fatalf("notch not centred: %+v on %+v", g.Notch, g.Screen)
	}
	t.Logf("%+v", g)
}
