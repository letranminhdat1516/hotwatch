package components_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	hwui "hotwatch/ui"
	"hotwatch/ui/components"
)

var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func pts(vs ...float64) []hub.Point {
	out := make([]hub.Point, len(vs))
	for i, v := range vs {
		out[i] = hub.Point{T: now.Add(time.Duration(i-len(vs)+1) * 2 * time.Second), V: v}
	}
	return out
}

var tint = ui.Hex("#6366f1")

func TestDotTimeChartPrintsUnitLatestValueAndTimeAxis(t *testing.T) {
	series := []components.Series{{Name: "Total", Pts: pts(10, 20, 37.5)}}
	tt := ui.NewTester(func(c *ui.Context) {
		components.DotTimeChart(c, "CPU usage", "%", series, now, tint)
	}, 600, 300)
	for _, want := range []string{"CPU usage", "%", "Total", "37.5 %", "−5m", "−1m", "now"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestDotTimeChartLegendPerSeriesInGivenOrder(t *testing.T) {
	series := []components.Series{{Name: "In", Pts: pts(100, 2048)}, {Name: "Out", Pts: pts(5, 1024)}}
	tt := ui.NewTester(func(c *ui.Context) {
		components.DotTimeChart(c, "Network", "B/s", series, now, tint)
	}, 600, 300)
	for _, want := range []string{"B/s", "In", "2.00 KB/s", "Out", "1.00 KB/s"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if i, o := slices.Index(tt.Texts(), "In"), slices.Index(tt.Texts(), "Out"); i < 0 || o < i {
		t.Errorf("legend not in the given order: %q", tt.Texts())
	}
}

func TestDotTimeChartOnlyTheWindow(t *testing.T) {
	series := []components.Series{{Name: "Total", Pts: []hub.Point{{T: now.Add(-6 * time.Minute), V: 99}}}}
	tt := ui.NewTester(func(c *ui.Context) {
		components.DotTimeChart(c, "CPU usage", "%", series, now, tint)
	}, 600, 300)
	if slices.Contains(tt.Texts(), "99.0 %") || !slices.Contains(tt.Texts(), "—") {
		t.Fatalf("point older than the window shown as current: %q", tt.Texts())
	}
	if !slices.Contains(tt.Texts(), "%") {
		t.Fatalf("unit missing without data: %q", tt.Texts())
	}
}

func TestDotTimeChartLatestSkipsNonFinite(t *testing.T) {
	series := []components.Series{{Name: "Total", Pts: pts(12, math.NaN())}}
	tt := ui.NewTester(func(c *ui.Context) {
		components.DotTimeChart(c, "CPU usage", "%", series, now, tint)
	}, 600, 300)
	if !slices.Contains(tt.Texts(), "12.0 %") {
		t.Fatalf("latest finite value not shown: %q", tt.Texts())
	}
}

func TestDotLayersShareOneScale(t *testing.T) {
	// Read peaks at 4 MB/s, Write at 1 MB/s: on a shared scale Write lights a
	// quarter of the rows, not all of them.
	read := []hub.Point{{T: now.Add(-time.Second), V: 4 << 20}}
	write := []hub.Point{{T: now.Add(-time.Second), V: 1 << 20}}
	lit := components.DotLayers([][]hub.Point{read, write}, now.Add(-time.Minute), now, 1, 16, 0)
	if len(lit) != 2 || lit[0][0] != 15 || lit[1][0] != 4 {
		t.Fatalf("lit rows %v, want [15] [4] (shared top with 8%% headroom)", lit)
	}
}

func TestBadgeStateColours(t *testing.T) {
	cases := []struct {
		state string
		label string
		color func(*ui.Theme) ui.Color
	}{
		{"Online", "Online", func(t *ui.Theme) ui.Color { return t.Success }},
		{"running", "Running", func(t *ui.Theme) ui.Color { return t.Success }},
		{"Stale", "Stale", func(t *ui.Theme) ui.Color { return t.Warning }},
		{"restarting", "Restarting", func(t *ui.Theme) ui.Color { return t.Warning }},
		{"needs-confirm", "Needs confirmation", func(t *ui.Theme) ui.Color { return t.Warning }},
		{"HostKeyUnknown", "Verify host key", func(t *ui.Theme) ui.Color { return t.Warning }},
		{"Offline", "Offline", func(t *ui.Theme) ui.Color { return t.Danger }},
		{"error", "Error", func(t *ui.Theme) ui.Color { return t.Danger }},
		{"exited", "Exited", func(t *ui.Theme) ui.Color { return t.Danger }},
		{"Unauthorized", "Not authorized", func(t *ui.Theme) ui.Color { return t.Danger }},
		{"HostKeyMismatch", "Host key changed", func(t *ui.Theme) ui.Color { return t.Danger }},
		{"paused", "Paused", func(t *ui.Theme) ui.Color { return t.Warning }},
		{"read-only", "Read-only", func(t *ui.Theme) ui.Color { return t.TextMuted }},
		{"Connecting", "Connecting", func(t *ui.Theme) ui.Color { return t.TextMuted }},
		{"normal", "Normal", func(t *ui.Theme) ui.Color { return t.Success }},
		{"warn", "Warning", func(t *ui.Theme) ui.Color { return t.Warning }},
		{"critical", "Critical", func(t *ui.Theme) ui.Color { return t.Danger }},
		{"up", "Up", func(t *ui.Theme) ui.Color { return t.Success }},
		{"down", "Down", func(t *ui.Theme) ui.Color { return t.TextMuted }},
	}
	for _, dark := range []bool{false, true} {
		th := ui.LightTheme()
		if dark {
			th = ui.DarkTheme()
		}
		for _, tc := range cases {
			if got, want := components.StateColor(th, tc.state), tc.color(th); got != want {
				t.Errorf("dark=%v %s: colour %v, want %v", dark, tc.state, got, want)
			}
		}
	}
	for _, tc := range cases {
		tt := ui.NewTester(func(c *ui.Context) { components.Badge(c, tc.state) }, 300, 100)
		if !slices.Contains(tt.Texts(), tc.label) {
			t.Errorf("%s: missing label %q in %q", tc.state, tc.label, tt.Texts())
		}
	}
}

func TestBannerAndEmptyState(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		components.Banner(c, components.BannerError, "Offline · last seen 2m ago")
		components.EmptyState(c, "No containers")
	}, 600, 300)
	for _, want := range []string{"Offline · last seen 2m ago", "No containers"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestBarRowShowsLabelAndValue(t *testing.T) {
	v := 42.5
	tt := ui.NewTester(func(c *ui.Context) {
		components.BarRow(c, "Core 1", &v, "42.5 %", tint)
		components.BarRow(c, "Core 2", nil, "—", tint)
	}, 400, 100)
	for _, want := range []string{"Core 1", "42.5 %", "Core 2", "—"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestStackedBarLegend(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		components.StackedBar(c, []components.Segment{
			{Label: "App", Value: 8 << 30}, {Label: "Free", Value: 4 << 30},
		})
	}, 500, 100)
	for _, want := range []string{"App", "8.00 GB", "Free", "4.00 GB"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestStackedDotChartPaintsEachLayerInItsColour(t *testing.T) {
	user, system := ui.Hex("#3b82f6"), ui.Hex("#ef4444")
	total := []hub.Point{{T: now, V: 100}}
	sys := []hub.Point{{T: now, V: 50}}
	var box ui.Rect
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(hwui.Theme(true))
		ui.Column(c).Width(400).Children(func() {
			box = components.StackedDotChart(c, []components.DotLayer{{Pts: sys, Color: system}, {Pts: total, Color: user}},
				now.Add(-time.Minute), now, 100, user, 120).Bounds()
		})
	}, 420, 200)
	tt.Frame()
	img := tt.Image()
	// The last column: the bottom half is System, the top half User.
	x := int(box.X + box.W - 10 - 3)
	at := func(y float32) ui.Color {
		r, g, b, _ := img.At(x, int(y)).RGBA()
		return ui.RGB(uint8(r>>8), uint8(g>>8), uint8(b>>8))
	}
	bottom, top := box.Y+120-10-3, box.Y+10+3
	if at(bottom) != system || at(top) != user {
		t.Fatalf("bottom %v (want system %v), top %v (want user %v)", at(bottom), system, at(top), user)
	}
	if !slices.Contains(tt.Texts(), "now") {
		t.Fatalf("no time axis: %q", tt.Texts())
	}
}
