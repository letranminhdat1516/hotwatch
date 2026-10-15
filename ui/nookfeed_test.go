package ui_test

import (
	"testing"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	hwui "hotwatch/ui"
)

func TestNookFeedAndCommands(t *testing.T) {
	hosts := []store.Host{{ID: "local", Name: "This Mac", Kind: "local"}, {ID: "vps", Name: "VPS", Kind: "agent"}}
	views := map[string]hub.HostView{
		"local": {Info: model.HostInfo{Caps: model.Caps{Battery: true}}, Snap: &model.Snapshot{}},
		"vps":   {Info: model.HostInfo{Caps: model.Caps{Docker: true}}, Snap: &model.Snapshot{}, Conn: hub.Conn{State: hub.Offline}},
	}
	var nav []string
	app := hwui.NewApp(func(id string) hub.HostView { return views[id] }, hosts, &hub.FakeRunner{}, func(h, p string) { nav = append(nav, h+"/"+p) })
	if app.Selected() != "local" || len(app.Hosts()) != 2 || app.HostView("vps").Conn.State != hub.Offline {
		t.Fatalf("feed: %q %d %v", app.Selected(), len(app.Hosts()), app.HostView("vps").Conn.State)
	}
	app.ShowPage("energy")
	app.SelectHost("vps")
	app.ShowPage("energy") // the VPS has no Energy page: ignored
	app.ShowPage("docker")
	want := []string{"local/energy", "vps/overview", "vps/docker"}
	if len(nav) != len(want) {
		t.Fatalf("nav %v, want %v", nav, want)
	}
	for i := range want {
		if nav[i] != want[i] {
			t.Fatalf("nav %v, want %v", nav, want)
		}
	}
	app.TogglePause()
	if !app.Paused() {
		t.Fatal("TogglePause did not pause")
	}
}
