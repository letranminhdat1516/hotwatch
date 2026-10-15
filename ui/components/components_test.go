package components_test

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
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

func TestTimeChartPrintsUnitAndLatestValue(t *testing.T) {
	series := map[string][]hub.Point{"Total": pts(10, 20, 37.5)}
	tt := ui.NewTester(func(c *ui.Context) {
		components.TimeChart(c, "CPU usage", "%", series, now)
	}, 600, 300)
	for _, want := range []string{"CPU usage", "%", "Total", "37.5 %"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestTimeChartLegendPerSeriesAndRates(t *testing.T) {
	series := map[string][]hub.Point{"In": pts(100, 2048), "Out": pts(5, 1024)}
	tt := ui.NewTester(func(c *ui.Context) {
		components.TimeChart(c, "Network", "B/s", series, now)
	}, 600, 300)
	for _, want := range []string{"B/s", "In", "2.0 KB/s", "Out", "1.0 KB/s"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if i, o := slices.Index(tt.Texts(), "In"), slices.Index(tt.Texts(), "Out"); i < 0 || o < i {
		t.Errorf("legend not in name order: %q", tt.Texts())
	}
}

func TestTimeChartOnlyLastFiveMinutes(t *testing.T) {
	series := map[string][]hub.Point{"Total": {{T: now.Add(-6 * time.Minute), V: 99}}}
	tt := ui.NewTester(func(c *ui.Context) {
		components.TimeChart(c, "CPU usage", "%", series, now)
	}, 600, 300)
	if slices.Contains(tt.Texts(), "99.0 %") || !slices.Contains(tt.Texts(), "—") {
		t.Fatalf("point older than the window shown as current: %q", tt.Texts())
	}
	if !slices.Contains(tt.Texts(), "%") {
		t.Fatalf("unit missing without data: %q", tt.Texts())
	}
}

func TestTimeChartLatestSkipsNonFinite(t *testing.T) {
	series := map[string][]hub.Point{"Total": pts(12, math.NaN())}
	tt := ui.NewTester(func(c *ui.Context) {
		components.TimeChart(c, "CPU usage", "%", series, now)
	}, 600, 300)
	if !slices.Contains(tt.Texts(), "12.0 %") {
		t.Fatalf("latest finite value not shown: %q", tt.Texts())
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
		components.BarRow(c, "Core 1", &v, "42.5 %")
		components.BarRow(c, "Core 2", nil, "—")
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
	for _, want := range []string{"App", "8.0 GB", "Free", "4.0 GB"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestTimeChartByteTicksAreRound(t *testing.T) {
	series := map[string][]hub.Point{"In": pts(100, 1<<20)}
	tt := ui.NewTester(func(c *ui.Context) {
		components.TimeChart(c, "Network", "B/s", series, now)
	}, 600, 400)
	for _, want := range []string{"0 B/s", "256.0 KB/s", "512.0 KB/s", "768.0 KB/s", "1.0 MB/s"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing tick %q in %q", want, tt.Texts())
		}
	}
	for _, x := range tt.Texts() {
		if strings.Contains(x, "819") || strings.Contains(x, "204") {
			t.Errorf("odd tick %q", x)
		}
	}
}
