package ui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"

	hwui "hotwatch/ui"
	"hotwatch/ui/components"
)

// The approved design (UI-DESIGN.md "Tokens", Claude Design canvas of
// 2026-10-15): rose accent, warm neutrals, 14 pt cards, metrics moved off
// red so none of them fights the accent.
func TestThemeTokensMatchTheApprovedDesign(t *testing.T) {
	light, dark := hwui.Theme(false), hwui.Theme(true)
	if light.Accent != ui.Hex("#e11d48") || dark.Accent != ui.Hex("#fb7185") {
		t.Fatalf("accent %v / %v differs from UI-DESIGN.md Tokens", light.Accent, dark.Accent)
	}
	if light.Background != ui.Hex("#f6f3f4") || dark.Background != ui.Hex("#121014") {
		t.Fatalf("background %v / %v differs from UI-DESIGN.md Tokens", light.Background, dark.Background)
	}
	if light.Radius != 14 || dark.Radius != 14 {
		t.Fatalf("card radius %v / %v, want 14", light.Radius, dark.Radius)
	}
	want := map[string]string{"cpu": "#6366f1", "memory": "#10b981", "gpu": "#f97316",
		"disk": "#0ea5e9", "network": "#14b8a6", "energy": "#f59e0b", "battery": "#f59e0b"}
	for key, hex := range want {
		got, ok := components.MetricColor(key)
		if !ok || got != ui.Hex(hex) {
			t.Errorf("metric %s = %v (%v), want %s", key, got, ok, hex)
		}
	}
	if _, ok := components.MetricColor("overview"); ok {
		t.Error("overview must have no metric colour")
	}
}
