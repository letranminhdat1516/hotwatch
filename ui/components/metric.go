package components

import "github.com/egoist/mygo/ui"

// metricHex is the colour of each hardware metric, shared by the sidebar
// tiles, the card headers and the charts so a metric reads the same
// everywhere. They are Apple's system colours (dark appearance), as the
// owner chose on 2026-10-15. Keys match the page keys; "battery" is the
// Overview's battery card.
var metricHex = map[string]string{
	"cpu":     "#0a84ff",
	"memory":  "#30d158",
	"gpu":     "#ff375f",
	"disk":    "#ff9f0a",
	"network": "#40c8e0",
	"energy":  "#ffd60a",
	"battery": "#30d158",
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
		Glyph(c, icon, 14, fg)
	})
}

// HeaderTile is the larger tile beside a card's title: 32 DIPs, radius 9,
// the icon at 18.
func HeaderTile(c *ui.Context, icon *ui.SVG, bg, fg ui.Color) {
	ui.Row(c).Size(32, 32).Radius(9).Background(bg).Center().Shrink(0).Children(func() {
		Glyph(c, icon, 18, fg)
	})
}
