package components

import "github.com/egoist/mygo/ui"

// metricHex is the accent colour of each hardware metric, shared by the
// sidebar tiles and the overview card headers so a metric reads the same
// everywhere. Keys match the page keys; "battery" shares the Energy page.
// None of them is red: the rose accent must stay the only red on screen
// (UI-DESIGN.md "Tokens").
var metricHex = map[string]string{
	"cpu":     "#6366f1",
	"memory":  "#10b981",
	"gpu":     "#f97316",
	"disk":    "#0ea5e9",
	"network": "#14b8a6",
	"energy":  "#f59e0b",
	"battery": "#f59e0b",
}

// MetricColor returns the metric's accent colour and whether it has one;
// pages without a colour (Overview, Processes, …) return ok=false.
func MetricColor(key string) (ui.Color, bool) {
	h, ok := metricHex[key]
	if !ok {
		return ui.Color{}, false
	}
	return ui.Hex(h), true
}

// IconTile draws a rounded square filled with bg holding icon in fg, 24 DIP.
func IconTile(c *ui.Context, icon *ui.SVG, bg, fg ui.Color) {
	ui.Row(c).Size(24, 24).Radius(6).Background(bg).Center().Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(fg)
	})
}
