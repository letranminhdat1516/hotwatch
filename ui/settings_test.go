package ui_test

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	"hotwatch/ui/format"
)

func TestSettingsPageChangesIntervalAndUnits(t *testing.T) {
	defer format.SetBitRates(false)
	rx, tx := 1024.0, 0.0
	v := hub.HostView{Snap: &model.Snapshot{Net: []model.Interface{{Name: "en0", Up: true, RxBps: &rx, TxBps: &tx}}}}
	app := testApp(v)
	var got []store.Settings
	app.SetSettings(store.Settings{IntervalMs: 2000})
	app.SetSettingsHook(func(s store.Settings) { got = append(got, s) })
	tt := ui.NewTester(app.View, 1200, 800)
	if !slices.Contains(tt.Texts(), "1.0 KB/s") {
		t.Fatalf("sidebar rate in bytes missing: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyComma)
	for _, s := range []string{"Update every", "Network speeds in", "Keyboard", "About", "Go"} {
		if !slices.Contains(tt.Texts(), s) {
			t.Fatalf("missing %q on the Settings page: %q", s, tt.Texts())
		}
	}
	if err := tt.Click("5 s"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].IntervalMs != 5000 {
		t.Fatalf("interval hook calls %+v, want one with 5000 ms", got)
	}
	if err := tt.Click("Bits/s"); err != nil {
		t.Fatal(err)
	}
	tt.Frame() // units apply on the next frame
	if len(got) != 2 || got[1].NetUnits != "bits" || got[1].IntervalMs != 5000 {
		t.Fatalf("units hook calls %+v, want bits with the interval kept", got)
	}
	// The sidebar's network rate follows at once: 1024 B/s is 8.2 kb/s.
	if !slices.Contains(tt.Texts(), "8.2 kb/s") || slices.Contains(tt.Texts(), "1.0 KB/s") {
		t.Fatalf("rates not in bits: %q", tt.Texts())
	}
	if err := tt.Click("Bytes/s"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !slices.Contains(tt.Texts(), "1.0 KB/s") {
		t.Fatalf("rates not back in bytes: %q", tt.Texts())
	}
}

func TestSettingsTurnsTheNookOff(t *testing.T) {
	app := testApp(hub.HostView{Snap: &model.Snapshot{}})
	var got []store.Settings
	app.SetSettings(store.Settings{IntervalMs: 2000})
	app.SetSettingsHook(func(s store.Settings) { got = append(got, s) })
	tt := ui.NewTester(app.View, 1200, 900)
	tt.Key(ui.Cmd, ui.KeyComma)
	if !slices.Contains(tt.Texts(), "Nook") {
		t.Fatalf("no Nook section: %q", tt.Texts())
	}
	if err := tt.Click("Hide"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(got) != 1 || !got[0].NookOff || got[0].IntervalMs != 2000 {
		t.Fatalf("hook calls %+v, want one with NookOff", got)
	}
}
