package ui_test

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	hwui "hotwatch/ui"
	"hotwatch/ui/hostpanel"
)

// switcherApp is a two-host app; withPanel installs a real host panel so
// the switcher offers host management.
func switcherApp(withPanel bool) *hwui.App {
	cfg := store.Config{Version: 1, Hosts: []store.Host{
		{ID: "mac", Name: "Mac", Kind: "local"},
		{ID: "vps", Name: "VPS", Kind: "agent", Conn: &store.Conn{Type: "direct", Address: "100.64.0.2", AgentPort: 7171}},
	}}
	views := map[string]hub.HostView{"mac": {Info: model.HostInfo{Caps: macCaps}}, "vps": {Info: model.HostInfo{Caps: vpsCaps}}}
	app := hwui.NewApp(func(id string) hub.HostView { return views[id] }, cfg.Hosts, &hub.FakeRunner{}, func(string, string) {})
	if withPanel {
		app.SetHostPanel(hostpanel.New(cfg, nil))
	}
	return app
}

func TestHostSwitcherListsOtherHostsAndOpensAddHost(t *testing.T) {
	tt := ui.NewTester(switcherApp(true).View, 1400, 800)
	if slices.Contains(tt.Texts(), "VPS") || slices.Contains(tt.Texts(), "Add host…") {
		t.Fatalf("switcher open before any click: %q", tt.Texts())
	}
	if err := tt.Click("Mac"); err != nil { // the current host opens the list
		t.Fatal(err)
	}
	for _, s := range []string{"VPS", "Add host…"} {
		if !slices.Contains(tt.Texts(), s) {
			t.Fatalf("missing %q after opening: %q", s, tt.Texts())
		}
	}
	if err := tt.Click("Add host…"); err != nil {
		t.Fatal(err)
	}
	// The add-host form opens (its Token field) and the list closes.
	if !slices.Contains(tt.Texts(), "Token") || slices.Contains(tt.Texts(), "Add host…") {
		t.Fatalf("add form not open: %q", tt.Texts())
	}
}

func TestHostSwitcherWithoutPanelOnlySwitches(t *testing.T) {
	tt := ui.NewTester(switcherApp(false).View, 1400, 800)
	if err := tt.Click("Mac"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Texts(), "VPS") || slices.Contains(tt.Texts(), "Add host…") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("VPS"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tt.Texts(), "Mac") && !slices.Contains(tt.Texts(), "VPS") {
		t.Fatalf("did not switch to VPS: %q", tt.Texts())
	}
}
