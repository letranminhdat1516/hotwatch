package hostpanel_test

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/store"
	"hotwatch/ui/hostpanel"
)

var t0 = time.Date(2026, 10, 27, 9, 30, 0, 0, time.Local)

// newManage renders the Hosts page (and the dialogs it opens) instead of the
// tree.
func newManage(t *testing.T, cfg store.Config) *harness {
	t.Helper()
	h := &harness{t: t, eff: &fakeEffects{secrets: store.NewMemSecrets()}, views: map[string]hub.HostView{}, sel: "vps"}
	h.p = hostpanel.New(cfg, h.eff)
	h.tt = ui.NewTester(func(c *ui.Context) {
		view := func(id string) hub.HostView { return h.views[id] }
		h.p.Manage(c, h.sel, view, t0)
		h.p.RenderDialogs(c, h.sel, view)
	}, 1000, 900)
	return h
}

func TestManageListsHostsWithDetailsByGroup(t *testing.T) {
	h := newManage(t, baseConfig())
	for _, s := range []string{"Local", "Servers", "Servers / Web", "This Mac", "VPS", "NAS",
		"Tailscale / LAN · 100.64.0.2:7171", "SSH · me@nas.lan:22", "New host", "New group"} {
		if !has(h.tt, s) {
			t.Errorf("missing %q in %q", s, h.tt.Texts())
		}
	}
}

func TestManageEditButtonOpensForm(t *testing.T) {
	h := newManage(t, baseConfig())
	h.click("Edit VPS")
	h.replace("Address", "100.64.0.9")
	h.click("Save")
	got, _ := hostByName(h.eff.last(t), "VPS")
	if got.Conn.Address != "100.64.0.9" || len(h.eff.changed) != 1 {
		t.Fatalf("host = %+v changed %v", got.Conn, h.eff.changed)
	}
}

func TestManageRemoveButtonAsksFirst(t *testing.T) {
	h := newManage(t, baseConfig())
	if _, ok := h.tt.Find("Remove This Mac"); ok {
		t.Fatal("This Mac has a Remove button")
	}
	h.click("Remove NAS")
	if len(h.eff.saved) != 0 {
		t.Fatal("removed without confirmation")
	}
	h.click("Remove")
	if _, ok := hostByName(h.eff.last(t), "NAS"); ok || !slices.Equal(h.eff.removed, []string{"nas"}) {
		t.Fatalf("not removed: %v", h.eff.removed)
	}
}

func TestManageNewHostAndGroupButtons(t *testing.T) {
	h := newManage(t, baseConfig())
	h.click("New host")
	if !has(h.tt, "Add host") {
		t.Fatalf("no add form: %q", h.tt.Texts())
	}
	h.click("Cancel")
	h.click("New group")
	h.tt.Type("Lab")
	h.click("Create")
	cfg := h.eff.last(t)
	if !slices.ContainsFunc(cfg.Groups, func(g store.Group) bool { return g.Name == "Lab" }) {
		t.Fatalf("groups = %+v", cfg.Groups)
	}
}

func TestManageGroupActionsAndMove(t *testing.T) {
	h := newManage(t, baseConfig())
	if err := h.tt.Click("Move VPS"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Local"); err != nil {
		t.Fatalf("%v (menu %q)", err, h.tt.Menu())
	}
	if g, _ := hostByName(h.eff.last(t), "VPS"); g.Group != "g-local" {
		t.Fatalf("group = %q", g.Group)
	}
	h.click("Rename Servers")
	h.replace("Name", "Cloud")
	h.click("Rename")
	if !has(h.tt, "Cloud") {
		t.Fatalf("not renamed: %q", h.tt.Texts())
	}
}

func TestManageSearchFilters(t *testing.T) {
	h := newManage(t, baseConfig())
	h.click("Search hosts")
	h.tt.Type("nas.lan")
	if has(h.tt, "VPS") || !has(h.tt, "NAS") {
		t.Fatalf("filter wrong: %q", h.tt.Texts())
	}
}

func TestManageClickSelectsAndShowsItsLog(t *testing.T) {
	h := newManage(t, baseConfig())
	h.views["vps"] = hub.HostView{Conn: hub.Conn{State: hub.Offline}, Log: []hub.LogEntry{
		{Time: t0.Add(-time.Minute), State: hub.Connecting, Detail: "Connecting"},
		{Time: t0.Add(-50 * time.Second), State: hub.Offline, Detail: "dial tcp 100.64.0.2:7171: connection refused"},
	}}
	h.tt.Frame()
	for _, s := range []string{"Connection log · VPS", "dial tcp 100.64.0.2:7171: connection refused", "09:29:10"} {
		if !has(h.tt, s) {
			t.Errorf("missing %q in %q", s, h.tt.Texts())
		}
	}
	h.click("NAS")
	if !slices.Equal(h.eff.selected, []string{"nas"}) {
		t.Fatalf("Select = %v", h.eff.selected)
	}
	h.sel = "nas"
	h.tt.Frame()
	if !has(h.tt, "No connection events yet.") {
		t.Fatalf("no empty state: %q", h.tt.Texts())
	}
}

func TestManageRemoveGroupButton(t *testing.T) {
	h := newManage(t, baseConfig())
	if has(h.tt, "•••") {
		t.Fatal("group actions still hidden behind •••")
	}
	h.click("Remove group Web")
	cfg := h.eff.last(t)
	if slices.ContainsFunc(cfg.Groups, func(g store.Group) bool { return g.ID == "g-web" }) {
		t.Fatal("group not removed")
	}
	if nas, _ := hostByName(cfg, "NAS"); nas.Group != "g-srv" {
		t.Fatalf("NAS moved to %q, want the parent", nas.Group)
	}
}

func TestManageWithoutGroupsHasNoNoGroupHeading(t *testing.T) {
	cfg := baseConfig()
	cfg.Groups = nil
	for i := range cfg.Hosts {
		cfg.Hosts[i].Group = ""
	}
	h := newManage(t, cfg)
	if has(h.tt, "No group") {
		t.Fatalf("heading shown with no groups: %q", h.tt.Texts())
	}
	if !has(h.tt, "VPS") || !has(h.tt, "NAS") {
		t.Fatalf("hosts missing: %q", h.tt.Texts())
	}
}
