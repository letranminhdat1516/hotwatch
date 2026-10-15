package ui_test

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
)

func TestChartWindowControlSetsWindow(t *testing.T) {
	app := testApp(hub.HostView{Snap: &model.Snapshot{}})
	tt := ui.NewTester(app.View, 1200, 800)
	if app.Window() != 5*time.Minute || components.ChartWindow() != 5*time.Minute {
		t.Fatalf("default window %v / %v, want 5m", app.Window(), components.ChartWindow())
	}
	for _, tc := range []struct {
		label string
		want  time.Duration
	}{{"15m", 15 * time.Minute}, {"1h", time.Hour}, {"1m", time.Minute}} {
		if err := tt.Click(tc.label); err != nil {
			t.Fatal(err)
		}
		if app.Window() != tc.want || components.ChartWindow() != tc.want {
			t.Fatalf("%s: window %v / charts %v, want %v", tc.label, app.Window(), components.ChartWindow(), tc.want)
		}
	}
	tt.Click("5m") // leave the package default for other tests
}

func TestPauseHoldsUpdatesUntilResumed(t *testing.T) {
	app := testApp(hub.HostView{Snap: &model.Snapshot{CPU: model.CPU{Total: 10}}})
	tt := ui.NewTester(app.View, 1200, 800)
	if err := tt.Click("Pause updates"); err != nil {
		t.Fatal(err)
	}
	if !app.Paused() {
		t.Fatal("not paused after the click")
	}
	app.Apply("h1", hub.HostView{Snap: &model.Snapshot{CPU: model.CPU{Total: 90}}})
	tt.Frame()
	if !slices.Contains(tt.Texts(), "10.0 %") || slices.Contains(tt.Texts(), "90.0 %") {
		t.Fatalf("paused view changed: %q", tt.Texts())
	}
	if err := tt.Click("Resume updates"); err != nil {
		t.Fatal(err)
	}
	if app.Paused() || !slices.Contains(tt.Texts(), "90.0 %") {
		t.Fatalf("resume did not show the held view: paused=%v %q", app.Paused(), tt.Texts())
	}
}

func TestPageSubtitleShowsFigures(t *testing.T) {
	v := hub.HostView{
		Info:  model.HostInfo{Hostname: "studio.local"},
		Snap:  &model.Snapshot{CPU: model.CPU{PerCore: make([]float64, 8)}, Memory: model.Memory{Total: 16 << 30, Used: 1}},
		Procs: []model.Process{{PID: 1, Threads: 3}, {PID: 2, Threads: 4}},
	}
	app := testApp(v)
	tt := ui.NewTester(app.View, 1200, 800)
	if !slices.Contains(tt.Texts(), "studio.local") {
		t.Fatalf("overview subtitle missing: %q", tt.Texts())
	}
	tt.Click("Processes")
	// The thread count comes only from the subtitle (the page's own toolbar
	// shows the process count), so the plural and the sum are both checked.
	if !slices.Contains(tt.Texts(), "2 processes · 7 threads") {
		t.Fatalf("processes subtitle missing: %q", tt.Texts())
	}
	tt.Click("CPU")
	if !slices.Contains(tt.Texts(), "8 cores") {
		t.Fatalf("cpu subtitle missing: %q", tt.Texts())
	}
}
