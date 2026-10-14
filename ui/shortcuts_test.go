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

type navRec struct{ host, page string }

func navApp(t *testing.T) (*hwui.App, *ui.Tester, *[]navRec) {
	t.Helper()
	hosts := []store.Host{{ID: "mac", Name: "Mac", Kind: "local"}, {ID: "vps", Name: "VPS", Kind: "agent"}}
	views := map[string]hub.HostView{"mac": {Info: model.HostInfo{Caps: macCaps}}, "vps": {Info: model.HostInfo{Caps: vpsCaps}}}
	var rec []navRec
	app := hwui.NewApp(func(id string) hub.HostView { return views[id] }, hosts, &hub.FakeRunner{},
		func(h, p string) { rec = append(rec, navRec{h, p}) })
	return app, ui.NewTester(app.View, 1400, 800), &rec
}

func lastNav(r []navRec) navRec {
	if len(r) == 0 {
		return navRec{}
	}
	return r[len(r)-1]
}

func TestCmdNumberSelectsPage(t *testing.T) {
	_, tt, rec := navApp(t)
	tt.Key(ui.Cmd, ui.Key2)
	if want := (navRec{"mac", "cpu"}); len(*rec) == 0 || lastNav(*rec) != want {
		t.Fatalf("selected %v, want last %v", *rec, want)
	}
	if n := count(tt, "CPU"); n != 2 {
		t.Fatalf("CPU title not shown: %q", tt.Texts())
	}
}

func TestCmdNumbersFollowVisiblePages(t *testing.T) {
	app, tt, rec := navApp(t)
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown) // VPS: no GPU, Energy
	*rec = nil
	// Overview CPU Memory Disk Network Processes Docker Settings
	tt.Key(ui.Cmd, ui.Key6)
	if got := lastNav(*rec); got != (navRec{"vps", "processes"}) {
		t.Fatalf("⌘6 on the VPS: %v", *rec)
	}
	n := len(*rec)
	tt.Key(ui.Cmd, ui.Key9) // only 8 visible pages: nothing
	if len(*rec) != n {
		t.Fatalf("⌘9 selected %v", *rec)
	}
	_ = app
}

func TestCmdCommaOpensSettings(t *testing.T) {
	_, tt, rec := navApp(t)
	tt.Key(ui.Cmd, ui.KeyComma)
	if got := lastNav(*rec); got != (navRec{"mac", "settings"}) {
		t.Fatalf("⌘, : %v", *rec)
	}
}

func TestAltCmdArrowsMoveBetweenHostsAndWrap(t *testing.T) {
	_, tt, rec := navApp(t)
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown)
	if got := lastNav(*rec).host; got != "vps" {
		t.Fatalf("next: %v", *rec)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown) // wraps to the first
	if got := lastNav(*rec).host; got != "mac" {
		t.Fatalf("wrap forward: %v", *rec)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyUp) // wraps to the last
	if got := lastNav(*rec).host; got != "vps" {
		t.Fatalf("wrap back: %v", *rec)
	}
}

func TestAltCmdArrowsFollowPanelOrder(t *testing.T) {
	// The panel shows groups before loose hosts and nests: a, c (in g), b.
	cfg := store.Config{Version: 1,
		Groups: []store.Group{{ID: "g", Name: "G"}},
		Hosts:  []store.Host{{ID: "a", Name: "A", Kind: "local"}, {ID: "b", Name: "B", Kind: "local"}, {ID: "c", Name: "C", Kind: "local", Group: "g"}}}
	var order []string
	app := hwui.NewApp(func(string) hub.HostView { return hub.HostView{} }, cfg.Hosts, &hub.FakeRunner{},
		func(h, _ string) { order = append(order, h) })
	app.SetHostPanel(hostpanel.New(cfg, nil))
	tt := ui.NewTester(app.View, 1400, 800)
	for range 3 {
		tt.Key(ui.Cmd|ui.Alt, ui.KeyDown)
	}
	// panel order: c (in g, groups first), a, b  -> start a: b, c, a
	if want := []string{"b", "c", "a"}; !slices.Equal(order, want) {
		t.Fatalf("visited %v, want %v", order, want)
	}
}

func TestCmdFFocusesProcessesSearch(t *testing.T) {
	_, tt, _ := navApp(t)
	tt.Key(ui.Cmd, ui.KeyF) // Overview: no-op
	if tt.Focused("Search") {
		t.Fatal("search focused off Processes")
	}
	tt.Click("Processes")
	tt.Key(ui.Cmd, ui.KeyF)
	if !tt.Focused("Search") {
		t.Fatalf("search not focused: %q", tt.Texts())
	}
}
