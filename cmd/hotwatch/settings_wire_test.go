package main

import (
	"log/slog"
	"slices"
	"testing"

	"hotwatch/store"
)

func TestSettingsHookSavesAndRetunesTheHub(t *testing.T) {
	c := store.Default()
	c.Settings.IntervalMs = 2000
	app := newFakeApp()
	w := newCore(c, testPaths(t), store.NewMemSecrets(), localFake, func(f func()) { f() }, slog.New(slog.DiscardHandler))
	w.attach(app, "")
	if app.settings.IntervalMs != 2000 || app.settingsHook == nil {
		t.Fatalf("attach: settings %+v, hook set %v", app.settings, app.settingsHook != nil)
	}
	fh := &fakeHub{}
	w.useHub(fh)
	s := app.settings
	s.IntervalMs, s.NetUnits = 500, "bits"
	app.settingsHook(s)
	if got := w.cfg.Config().Settings; got.IntervalMs != 500 || got.NetUnits != "bits" {
		t.Fatalf("saved settings %+v", got)
	}
	fh.mu.Lock()
	calls := slices.Clone(fh.calls)
	fh.mu.Unlock()
	if !slices.Contains(calls, hubCall{"interval", "500ms"}) {
		t.Fatalf("hub calls %v, want interval 500ms", calls)
	}
}
