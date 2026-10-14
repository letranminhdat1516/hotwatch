package ui_test

import (
	"slices"
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
	if !slices.Contains(tt.Texts(), "37.5 %") {
		t.Fatalf("missing CPU value in %q", tt.Texts())
	}
	for _, want := range []string{"CPU", "Memory", "Disk", "Network"} {
		// one in the top bar, one as the card title
		if n := count(tt, want); n != 2 {
			t.Fatalf("%q shown %d times, want 2: %q", want, n, tt.Texts())
		}
	}
	if slices.Contains(tt.Texts(), "Battery") {
		t.Fatal("battery card shown without a battery")
	}
	if slices.Contains(tt.Texts(), "Apple M3") {
		t.Fatal("gpu card shown without a gpu")
	}
}

func TestOverviewUnknownRatesAreDashes(t *testing.T) {
	snap := &model.Snapshot{Net: []model.Interface{{Name: "en0", Up: true}}}
	app := testApp(hub.HostView{Snap: snap})
	tt := ui.NewTester(app.View, 1200, 800)
	if !slices.Contains(tt.Texts(), "In —") || !slices.Contains(tt.Texts(), "Out —") {
		t.Fatalf("nil rates must render as a dash: %q", tt.Texts())
	}
	if slices.Contains(tt.Texts(), "In 0 B/s") {
		t.Fatalf("unknown rate rendered as zero: %q", tt.Texts())
	}
}

func fullSnap() *model.Snapshot {
	return &model.Snapshot{
		CPU:     model.CPU{Total: 37.5, Load: [3]float64{1, 2, 3}},
		Memory:  model.Memory{Total: 8 << 30, Used: 4 << 30},
		Disks:   []model.Volume{{Mount: "/", Total: 100 << 30, Used: 25 << 30}},
		Net:     []model.Interface{{Name: "en0", Up: true, RxBps: f64(2048), TxBps: f64(1024)}},
		Battery: &model.Battery{ChargePct: 80},
	}
}

func TestOverviewPartialSectionIsUnknown(t *testing.T) {
	cases := []struct {
		section string
		want    []string // still shown
		absent  []string
		unknown []string
	}{
		{"cpu", []string{"50.0 %", "25.0 %", "In 2.0 KB/s", "80.0 %"}, []string{"37.5 %", "Load 1.00 / 2.00 / 3.00"}, []string{"—", "Load —"}},
		{"net", []string{"37.5 %", "50.0 %", "80.0 %"}, []string{"In 2.0 KB/s"}, []string{"In —", "Out —"}},
		{"battery", []string{"37.5 %", "50.0 %", "In 2.0 KB/s"}, []string{"80.0 %"}, []string{"—"}},
	}
	for _, tc := range cases {
		snap := fullSnap()
		snap.Partial = []string{tc.section}
		app := testApp(hub.HostView{Snap: snap})
		tt := ui.NewTester(app.View, 1200, 800)
		for _, w := range append(tc.want, tc.unknown...) {
			if !slices.Contains(tt.Texts(), w) {
				t.Errorf("%s partial: missing %q in %q", tc.section, w, tt.Texts())
			}
		}
		for _, w := range tc.absent {
			if slices.Contains(tt.Texts(), w) {
				t.Errorf("%s partial: unexpected %q in %q", tc.section, w, tt.Texts())
			}
		}
	}
}

func TestOverviewNetworkSkipsLoopbackAndDownInterfaces(t *testing.T) {
	snap := &model.Snapshot{Net: []model.Interface{
		{Name: "lo", Up: true, RxBps: f64(5000)},
		{Name: "lo0", Up: true, RxBps: f64(9000), TxBps: f64(9000)},
		{Name: "en1", Up: false, RxBps: f64(7000)},
		{Name: "en0", Up: true, RxBps: f64(1024), TxBps: f64(0)},
	}}
	tt := ui.NewTester(testApp(hub.HostView{Snap: snap}).View, 1200, 800)
	if !slices.Contains(tt.Texts(), "In 1.0 KB/s") || !slices.Contains(tt.Texts(), "Out 0 B/s") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestOverviewOneCardPerGPU(t *testing.T) {
	snap := &model.Snapshot{GPUs: []model.GPU{{Name: "GPU A", UtilPct: f64(10)}, {Name: "GPU B", UtilPct: f64(20)}, {UtilPct: f64(30)}}}
	tt := ui.NewTester(testApp(hub.HostView{Snap: snap}).View, 1200, 800)
	for _, w := range []string{"GPU A", "10.0 %", "GPU B", "20.0 %", "GPU 3", "30.0 %"} {
		if !slices.Contains(tt.Texts(), w) {
			t.Fatalf("missing %q in %q", w, tt.Texts())
		}
	}
}

func TestOverviewWaitingForData(t *testing.T) {
	app := testApp(hub.HostView{})
	tt := ui.NewTester(app.View, 1200, 800)
	if !slices.Contains(tt.Texts(), "Waiting for data…") {
		t.Fatalf("no waiting state: %q", tt.Texts())
	}
	if count(tt, "CPU") != 1 { // the top bar item only
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
	for _, want := range []string{"Apple M3", "12.5 %", "Battery", "80.0 %", "Uptime 3h 2m"} {
		if !slices.Contains(tt.Texts(), want) {
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
	if !slices.Contains(tt.Texts(), "50.0 %") || slices.Contains(tt.Texts(), "99.0 %") {
		t.Fatalf("Apply did not copy: %q", tt.Texts())
	}
}
