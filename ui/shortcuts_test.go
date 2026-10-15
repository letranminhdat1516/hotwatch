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

func navApp(t *testing.T) (*ui.Tester, *[]navRec) {
	t.Helper()
	hosts := []store.Host{{ID: "mac", Name: "Mac", Kind: "local"}, {ID: "vps", Name: "VPS", Kind: "agent"}}
	views := map[string]hub.HostView{"mac": {Info: model.HostInfo{Caps: macCaps}}, "vps": {Info: model.HostInfo{Caps: vpsCaps}}}
	var rec []navRec
	app := hwui.NewApp(func(id string) hub.HostView { return views[id] }, hosts, &hub.FakeRunner{},
		func(h, p string) { rec = append(rec, navRec{h, p}) })
	return ui.NewTester(app.View, 1400, 800), &rec
}

func lastNav(r []navRec) navRec {
	if len(r) == 0 {
		return navRec{}
	}
	return r[len(r)-1]
}

func TestCmdNumberSelectsPage(t *testing.T) {
	tt, rec := navApp(t)
	tt.Key(ui.Cmd, ui.Key3) // sidebar order: Overview, Processes, CPU, …
	if want := (navRec{"mac", "cpu"}); len(*rec) == 0 || lastNav(*rec) != want {
		t.Fatalf("selected %v, want last %v", *rec, want)
	}
	if n := count(tt, "CPU"); n != 2 {
		t.Fatalf("CPU title not shown: %q", tt.Texts())
	}
}

func TestCmdNumbersFollowVisiblePages(t *testing.T) {
	tt, rec := navApp(t)
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown) // VPS: no GPU, Energy
	*rec = nil
	// Overview Processes CPU Memory Disk Network Docker Settings
	tt.Key(ui.Cmd, ui.Key6)
	if got := lastNav(*rec); got != (navRec{"vps", "network"}) {
		t.Fatalf("⌘6 on the VPS: %v", *rec)
	}
	if count(tt, "Energy") != 0 {
		t.Fatalf("Energy shown on the VPS: %q", tt.Texts())
	}
	n := len(*rec)
	tt.Key(ui.Cmd, ui.Key9) // only 8 visible pages: nothing
	if len(*rec) != n {
		t.Fatalf("⌘9 selected %v", *rec)
	}
}

func TestCmdCommaOpensSettings(t *testing.T) {
	tt, rec := navApp(t)
	tt.Key(ui.Cmd, ui.KeyComma)
	if got := lastNav(*rec); got != (navRec{"mac", "settings"}) {
		t.Fatalf("⌘, : %v", *rec)
	}
}

func TestAltCmdArrowsMoveBetweenHostsAndWrap(t *testing.T) {
	tt, rec := navApp(t)
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
	tt, _ := navApp(t)
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

func procHosts(t *testing.T) (*ui.Tester, *[]navRec) {
	t.Helper()
	hosts := []store.Host{{ID: "a", Name: "A", Kind: "local"}, {ID: "b", Name: "B", Kind: "agent"}}
	views := map[string]hub.HostView{
		"a": {Info: model.HostInfo{Caps: macCaps}, Procs: []model.Process{{PID: 1, Name: "procA"}}},
		"b": {Info: model.HostInfo{Caps: vpsCaps}, Procs: []model.Process{{PID: 2, Name: "procB"}}},
	}
	var rec []navRec
	app := hwui.NewApp(func(id string) hub.HostView { return views[id] }, hosts, &hub.FakeRunner{},
		func(h, p string) { rec = append(rec, navRec{h, p}) })
	return ui.NewTester(app.View, 1400, 800), &rec
}

func TestHostSwitchShowsOnlyTheNewHostsRows(t *testing.T) {
	tt, rec := procHosts(t)
	tt.Click("Processes")
	if !slices.Contains(tt.Texts(), "procA") {
		t.Fatalf("host A rows missing: %q", tt.Texts())
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown)
	if slices.Contains(tt.Texts(), "procA") || !slices.Contains(tt.Texts(), "procB") {
		t.Fatalf("after switch: %q", tt.Texts())
	}
	if got := lastNav(*rec); got != (navRec{"b", "processes"}) {
		t.Fatalf("selected %v", *rec)
	}
}

func TestHostSwitchResolvesPageOnce(t *testing.T) {
	tt, rec := procHosts(t)
	tt.Click("GPU")
	*rec = nil
	tt.Key(ui.Cmd|ui.Alt, ui.KeyDown)
	if want := []navRec{{"b", "overview"}}; !slices.Equal(*rec, want) {
		t.Fatalf("onSelect calls %v, want %v", *rec, want)
	}
}

func TestShortcutsWorkWhileSearchFocused(t *testing.T) {
	tt, rec := navApp(t)
	tt.Click("Processes")
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("12")
	if got := lastNav(*rec).page; got != "processes" {
		t.Fatalf("typing digits changed the page: %v", *rec)
	}
	tt.Key(ui.Cmd, ui.Key3)
	if got := lastNav(*rec).page; got != "cpu" {
		t.Fatalf("Cmd+3 with search focused: %v", *rec)
	}
}

func TestEveryVisiblePageRenders(t *testing.T) {
	const notBuilt = "This page is not built yet."
	keys := []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8}
	for name, tc := range map[string]struct {
		caps   model.Caps
		titles []string
	}{
		"mac": {model.Caps{GPU: true, Battery: true, Processes: true},
			[]string{"Overview", "Processes", "CPU", "Memory", "GPU", "Disk", "Network", "Energy"}},
		"vps": {model.Caps{Docker: true, Processes: true},
			[]string{"Overview", "Processes", "CPU", "Memory", "Disk", "Network", "Docker"}},
	} {
		t.Run(name, func(t *testing.T) {
			hosts := []store.Host{{ID: "h", Name: "Host", Kind: "agent"}}
			v := hub.HostView{Info: model.HostInfo{Caps: tc.caps}, Snap: &model.Snapshot{}}
			app := hwui.NewApp(func(string) hub.HostView { return v }, hosts, &hub.FakeRunner{}, func(string, string) {})
			tt := ui.NewTester(app.View, 1400, 900)
			for i, title := range tc.titles {
				if i > 0 {
					tt.Key(ui.Cmd, keys[i])
				}
				if n := count(tt, title); n < 2 {
					t.Errorf("%s: title shown %d times, want sidebar row and page title: %q", title, n, tt.Texts())
				}
				if count(tt, notBuilt) != 0 {
					t.Errorf("%s: shown as not built yet", title)
				}
			}
			tt.Key(ui.Cmd, ui.KeyComma)
			if count(tt, notBuilt) != 0 || count(tt, "Update every") != 1 {
				t.Errorf("settings page not rendered: %q", tt.Texts())
			}
		})
	}
}
