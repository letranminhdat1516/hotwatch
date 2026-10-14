package ui_test

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	hwui "hotwatch/ui"
)

func f64(v float64) *float64 { return &v }

func testApp(v hub.HostView) *hwui.App {
	hosts := []store.Host{{ID: "h1", Name: "Mac", Kind: "local"}}
	return hwui.NewApp(func(string) hub.HostView { return v }, hosts, &hub.FakeRunner{}, func(string, string) {})
}

func TestOverviewShowsCardsAndHidesAbsentHardware(t *testing.T) {
	app := testApp(hub.HostView{Snap: &model.Snapshot{CPU: model.CPU{Total: 37.5}, Battery: nil}})
	tt := ui.NewTester(app.View, 1200, 800)
	for _, want := range []string{"CPU", "37.5 %", "Memory", "Disk", "Network"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Battery") {
		t.Fatal("battery card shown without a battery")
	}
	if tt.HasText("GPU") {
		t.Fatal("gpu card shown without a gpu")
	}
}

func TestOverviewUnknownRatesAreDashes(t *testing.T) {
	snap := &model.Snapshot{Net: []model.Interface{{Name: "en0"}}}
	app := testApp(hub.HostView{Snap: snap})
	tt := ui.NewTester(app.View, 1200, 800)
	if !tt.HasText("In —") || !tt.HasText("Out —") {
		t.Fatalf("nil rates must render as a dash: %q", tt.Texts())
	}
	if tt.HasText("In 0 B/s") {
		t.Fatalf("unknown rate rendered as zero: %q", tt.Texts())
	}
}

func TestOverviewPartialSectionIsUnknown(t *testing.T) {
	snap := &model.Snapshot{CPU: model.CPU{Total: 0}, Partial: []string{"cpu"}}
	app := testApp(hub.HostView{Snap: snap})
	tt := ui.NewTester(app.View, 1200, 800)
	if tt.HasText("0.0 %") {
		t.Fatalf("partial cpu shown as 0: %q", tt.Texts())
	}
}

func TestOverviewWaitingForData(t *testing.T) {
	app := testApp(hub.HostView{})
	tt := ui.NewTester(app.View, 1200, 800)
	if !tt.HasText("Waiting for data…") {
		t.Fatalf("no waiting state: %q", tt.Texts())
	}
	if tt.HasText("CPU") {
		t.Fatalf("cards shown before first snapshot: %q", tt.Texts())
	}
}

func TestOverviewShowsGPUAndBatteryWhenPresent(t *testing.T) {
	snap := &model.Snapshot{
		GPUs:    []model.GPU{{Name: "Apple M3", UtilPct: f64(12.5)}},
		Battery: &model.Battery{ChargePct: 80},
		Uptime:  3*time.Hour + 2*time.Minute,
	}
	app := testApp(hub.HostView{Snap: snap})
	tt := ui.NewTester(app.View, 1200, 800)
	for _, want := range []string{"GPU", "12.5 %", "Battery", "80.0 %", "Uptime 3h 2m"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestApplyCopiesAndReplaces(t *testing.T) {
	app := testApp(hub.HostView{})
	tt := ui.NewTester(app.View, 1200, 800)
	snap := &model.Snapshot{CPU: model.CPU{Total: 50}}
	app.Apply("h1", hub.HostView{Snap: snap})
	snap.CPU.Total = 99 // mutation after Apply must not leak into the view
	tt.Frame()
	if !tt.HasText("50.0 %") || tt.HasText("99.0 %") {
		t.Fatalf("Apply did not copy: %q", tt.Texts())
	}
}
