package hostpanel_test

import (
	"errors"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/store"
	"hotwatch/transport"
	"hotwatch/ui/hostpanel"
)

const token43 = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ" // 43 chars

type fakeEffects struct {
	saved    []store.Config
	secrets  *store.MemSecrets
	setCalls int
	trusted  []*transport.HostKeyError
	trustErr error
	added    []store.Host
	changed  []store.Host
	removed  []string
	selected []string
}

func (f *fakeEffects) SaveConfig(c store.Config) error { f.saved = append(f.saved, c); return nil }
func (f *fakeEffects) SetToken(id, tok string) error {
	f.setCalls++
	return f.secrets.Set(store.TokenKey(id), tok)
}
func (f *fakeEffects) TrustHostKey(id string, hk *transport.HostKeyError) error {
	f.trusted = append(f.trusted, hk)
	return f.trustErr
}
func (f *fakeEffects) HostAdded(h store.Host)   { f.added = append(f.added, h) }
func (f *fakeEffects) HostChanged(h store.Host) { f.changed = append(f.changed, h) }
func (f *fakeEffects) HostRemoved(id string)    { f.removed = append(f.removed, id) }
func (f *fakeEffects) Select(id string)         { f.selected = append(f.selected, id) }

func (f *fakeEffects) last(t *testing.T) store.Config {
	t.Helper()
	if len(f.saved) == 0 {
		t.Fatal("config was not saved")
	}
	return f.saved[len(f.saved)-1]
}

func baseConfig() store.Config {
	return store.Config{
		Version: 1,
		Groups: []store.Group{
			{ID: "g-local", Name: "Local"},
			{ID: "g-srv", Name: "Servers"},
			{ID: "g-web", Name: "Web", Parent: "g-srv"},
		},
		Hosts: []store.Host{
			{ID: "local", Name: "This Mac", Kind: "local", Group: "g-local"},
			{ID: "vps", Name: "VPS", Kind: "agent", Group: "g-srv", Conn: &store.Conn{Type: "direct", Address: "100.64.0.2", AgentPort: 7171}},
			{ID: "nas", Name: "NAS", Kind: "agent", Group: "g-web", Conn: &store.Conn{Type: "ssh", SSHHost: "nas.lan", SSHPort: 22, SSHUser: "me", AgentPort: 7171}},
		},
		UI: store.UIState{OpenGroups: []string{"g-local", "g-srv", "g-web"}},
	}
}

type harness struct {
	t     *testing.T
	eff   *fakeEffects
	p     *hostpanel.Panel
	views map[string]hub.HostView
	sel   string
	tt    *ui.Tester
}

func newHarness(t *testing.T, cfg store.Config) *harness {
	t.Helper()
	h := &harness{t: t, eff: &fakeEffects{secrets: store.NewMemSecrets()}, views: map[string]hub.HostView{}, sel: "local"}
	h.p = hostpanel.New(cfg, h.eff)
	h.tt = ui.NewTester(func(c *ui.Context) {
		h.p.Render(c, h.sel, func(id string) hub.HostView { return h.views[id] })
	}, 900, 700)
	return h
}

func (h *harness) click(s string) {
	h.t.Helper()
	if err := h.tt.Click(s); err != nil {
		h.t.Fatalf("%v (texts %q)", err, h.tt.Texts())
	}
}

func (h *harness) typeInto(label, text string) {
	h.t.Helper()
	h.click(label)
	h.tt.Type(text)
}

func (h *harness) replace(label, text string) {
	h.t.Helper()
	h.click(label)
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type(text)
}

func (h *harness) menu(rightClick string, path ...string) {
	h.t.Helper()
	if err := h.tt.RightClick(rightClick); err != nil {
		h.t.Fatalf("%v (texts %q)", err, h.tt.Texts())
	}
	if err := h.tt.ChooseMenuItem(path...); err != nil {
		h.t.Fatalf("%v (menu %q)", err, h.tt.Menu())
	}
}

func (h *harness) addMenu(item string) {
	h.t.Helper()
	h.click("+")
	if err := h.tt.ChooseMenuItem(item); err != nil {
		h.t.Fatalf("%v (menu %q)", err, h.tt.Menu())
	}
}

func hostByName(c store.Config, name string) (store.Host, bool) {
	for _, h := range c.Hosts {
		if h.Name == name {
			return h, true
		}
	}
	return store.Host{}, false
}

func has(tt *ui.Tester, s string) bool { return slices.Contains(tt.Texts(), s) }

func TestAddSSHHostSavesConfigAndToken(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.addMenu("Add host…")
	h.typeInto("Name", "Box")
	h.click("SSH tunnel")
	h.typeInto("Host", "box.example.com")
	h.typeInto("Token", token43)
	h.click("Add host")

	got, ok := hostByName(h.eff.last(t), "Box")
	if !ok {
		t.Fatalf("host not saved: %+v", h.eff.last(t).Hosts)
	}
	c := got.Conn
	if got.Kind != "agent" || c == nil || c.Type != "ssh" || c.SSHHost != "box.example.com" || c.SSHPort != 22 || c.AgentPort != 7171 || c.SSHUser == "" {
		t.Fatalf("conn = %+v kind %q", c, got.Kind)
	}
	if v, err := h.eff.secrets.Get(store.TokenKey(got.ID)); err != nil || v != token43 {
		t.Fatalf("secret = %q, %v", v, err)
	}
	if len(h.eff.added) != 1 || h.eff.added[0].ID != got.ID {
		t.Fatalf("HostAdded = %+v", h.eff.added)
	}
	if has(h.tt, "Add host") {
		t.Errorf("dialog still open: %q", h.tt.Texts())
	}
}

func TestAddDirectHostDefaultsPort(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.addMenu("Add host…")
	h.typeInto("Name", "Laptop")
	h.typeInto("Address", "100.64.0.9")
	h.typeInto("Token", token43)
	h.click("Add host")
	got, _ := hostByName(h.eff.last(t), "Laptop")
	if got.Conn == nil || got.Conn.Type != "direct" || got.Conn.Address != "100.64.0.9" || got.Conn.AgentPort != 7171 {
		t.Fatalf("conn = %+v", got.Conn)
	}
}

func TestValidationMessages(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.addMenu("Add host…")
	h.click("Add host")
	for _, want := range []string{"Enter a name.", "Enter an address.", "The token must be at least 32 characters."} {
		if !has(h.tt, want) {
			t.Errorf("missing %q in %q", want, h.tt.Texts())
		}
	}
	if len(h.eff.saved) != 0 {
		t.Error("invalid form saved the config")
	}
	h.click("SSH tunnel")
	h.replace("Port", "0")
	h.typeInto("Token", "short")
	for _, want := range []string{"Enter the SSH host.", "Enter a port from 1 to 65535."} {
		if !has(h.tt, want) {
			t.Errorf("missing %q in %q", want, h.tt.Texts())
		}
	}
	if has(h.tt, "Enter an address.") {
		t.Error("direct field error shown for SSH")
	}
}

func TestPortOutOfRange(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.addMenu("Add host…")
	h.typeInto("Name", "X")
	h.typeInto("Address", "a")
	h.typeInto("Token", token43)
	h.replace("Port", "70000")
	h.click("Add host")
	if !has(h.tt, "Enter a port from 1 to 65535.") || len(h.eff.saved) != 0 {
		t.Fatalf("texts %q saved %d", h.tt.Texts(), len(h.eff.saved))
	}
}

func TestEditWithEmptyTokenKeepsToken(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.menu("VPS", "Edit…")
	h.typeInto("Name", "2")
	h.click("Save")
	got, _ := hostByName(h.eff.last(t), "VPS2")
	if got.ID != "vps" || got.Conn.Address != "100.64.0.2" {
		t.Fatalf("host = %+v", got)
	}
	if h.eff.setCalls != 0 {
		t.Errorf("SetToken called %d times", h.eff.setCalls)
	}
	if len(h.eff.changed) != 1 || h.eff.changed[0].Name != "VPS2" {
		t.Errorf("HostChanged = %+v", h.eff.changed)
	}
}

func TestEditWithNewTokenReplacesIt(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.menu("VPS", "Edit…")
	h.typeInto("Token", token43)
	h.click("Save")
	if v, _ := h.eff.secrets.Get(store.TokenKey("vps")); v != token43 {
		t.Fatalf("secret = %q", v)
	}
}

func TestThisMacCanOnlyBeRenamed(t *testing.T) {
	h := newHarness(t, baseConfig())
	if err := h.tt.RightClick("This Mac"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Remove…"); err == nil {
		t.Fatal("This Mac could be removed")
	}
	if err := h.tt.ChooseMenuItem("Edit…"); err != nil {
		t.Fatal(err)
	}
	for _, no := range []string{"Address", "Token", "Connection"} {
		if has(h.tt, no) {
			t.Errorf("%q editable for This Mac", no)
		}
	}
	h.typeInto("Name", "!")
	h.click("Save")
	got, _ := hostByName(h.eff.last(t), "This Mac!")
	if got.Kind != "local" || got.Conn != nil {
		t.Fatalf("host = %+v", got)
	}
	if h.eff.setCalls != 0 {
		t.Error("token set for local host")
	}
}

func TestRemoveHostAsksFirst(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.menu("VPS", "Remove…")
	if len(h.eff.saved) != 0 {
		t.Fatal("removed without confirmation")
	}
	h.click("Cancel")
	if len(h.eff.saved) != 0 {
		t.Fatal("cancel removed the host")
	}
	h.menu("VPS", "Remove…")
	h.click("Remove")
	if _, ok := hostByName(h.eff.last(t), "VPS"); ok {
		t.Fatal("host still in config")
	}
	if !slices.Equal(h.eff.removed, []string{"vps"}) {
		t.Fatalf("HostRemoved = %q", h.eff.removed)
	}
}

func TestMoveToGroup(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.menu("VPS", "Move to group", "Local")
	if g, _ := hostByName(h.eff.last(t), "VPS"); g.Group != "g-local" {
		t.Fatalf("group = %q", g.Group)
	}
	h.menu("VPS", "Move to group", "No group")
	if g, _ := hostByName(h.eff.last(t), "VPS"); g.Group != "" {
		t.Fatalf("group = %q", g.Group)
	}
}

func TestCollapsingGroupHidesHostsAndSavesState(t *testing.T) {
	h := newHarness(t, baseConfig())
	if !has(h.tt, "VPS") {
		t.Fatalf("VPS hidden while open: %q", h.tt.Texts())
	}
	h.click("Servers")
	if has(h.tt, "VPS") || has(h.tt, "NAS") {
		t.Fatalf("hosts shown in a collapsed group: %q", h.tt.Texts())
	}
	if open := h.eff.last(t).UI.OpenGroups; slices.Contains(open, "g-srv") || !slices.Contains(open, "g-local") {
		t.Fatalf("OpenGroups = %q", open)
	}
	h.click("Servers")
	if !has(h.tt, "VPS") || !slices.Contains(h.eff.last(t).UI.OpenGroups, "g-srv") {
		t.Fatal("did not reopen")
	}
}

func TestClickingHostSelects(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.click("VPS")
	if !slices.Equal(h.eff.selected, []string{"vps"}) {
		t.Fatalf("Select = %q", h.eff.selected)
	}
}

func TestNewGroupAndSubgroupAndRename(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.addMenu("New group…")
	h.tt.Type("Lab")
	h.click("Create")
	var lab store.Group
	for _, g := range h.eff.last(t).Groups {
		if g.Name == "Lab" {
			lab = g
		}
	}
	if lab.ID == "" || lab.Parent != "" || !slices.Contains(h.eff.last(t).UI.OpenGroups, lab.ID) {
		t.Fatalf("group = %+v open %q", lab, h.eff.last(t).UI.OpenGroups)
	}

	h.menu("Lab", "New subgroup…")
	h.tt.Type("Racks")
	h.click("Create")
	var sub store.Group
	for _, g := range h.eff.last(t).Groups {
		if g.Name == "Racks" {
			sub = g
		}
	}
	if sub.Parent != lab.ID {
		t.Fatalf("subgroup = %+v", sub)
	}

	h.menu("Lab", "Rename…")
	h.tt.Key(ui.Cmd, ui.KeyA)
	h.tt.Type("Lab2")
	h.click("Rename")
	if !has(h.tt, "Lab2") || has(h.tt, "Lab") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
}

func TestRemoveGroupMovesHostsToParent(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.menu("Web", "Remove group")
	c := h.eff.last(t)
	if nas, _ := hostByName(c, "NAS"); nas.Group != "g-srv" {
		t.Fatalf("NAS group = %q", nas.Group)
	}
	if !has(h.tt, "NAS") {
		t.Fatal("NAS disappeared")
	}
}

func unknownKey() *transport.HostKeyError {
	return &transport.HostKeyError{Host: "nas.lan", Fingerprint: "SHA256:abcDEF123", Mismatch: false}
}

func TestUnknownHostKeyDialogTrustPassesTheSameError(t *testing.T) {
	h := newHarness(t, baseConfig())
	hk := unknownKey()
	h.sel = "nas"
	h.views["nas"] = hub.HostView{Conn: hub.Conn{State: hub.HostKeyUnknown}, HostKey: hk}
	h.tt.Frame()
	if !has(h.tt, "SHA256:abcDEF123") || !h.tt.HasText("nas.lan is not known yet") || !h.tt.HasText("ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	h.click("Trust")
	if len(h.eff.trusted) != 1 || h.eff.trusted[0] != hk {
		t.Fatalf("TrustHostKey got %p, want %p", h.eff.trusted, hk)
	}
	if has(h.tt, "Trust") {
		t.Error("dialog still open after trusting")
	}
}

func TestUnknownHostKeyCancelDoesNotTrust(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.sel = "nas"
	h.views["nas"] = hub.HostView{Conn: hub.Conn{State: hub.HostKeyUnknown}, HostKey: unknownKey()}
	h.tt.Frame()
	h.click("Cancel")
	h.tt.Frame()
	if len(h.eff.trusted) != 0 {
		t.Fatal("cancel trusted the key")
	}
	if has(h.tt, "Trust") {
		t.Fatal("dialog reopened by itself")
	}
	h.click("Verify host key…")
	if !has(h.tt, "Trust") {
		t.Fatalf("cannot reopen: %q", h.tt.Texts())
	}
}

func TestTrustFailureIsShown(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.eff.trustErr = errors.New("disk full")
	h.sel = "nas"
	h.views["nas"] = hub.HostView{Conn: hub.Conn{State: hub.HostKeyUnknown}, HostKey: unknownKey()}
	h.tt.Frame()
	h.click("Trust")
	if !h.tt.HasText("disk full") || !has(h.tt, "Trust") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
}

func TestHostKeyMismatchHasNoTrustButton(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.sel = "nas"
	hk := unknownKey()
	hk.Mismatch = true
	h.views["nas"] = hub.HostView{Conn: hub.Conn{State: hub.HostKeyMismatch}, HostKey: hk}
	h.tt.Frame()
	if !has(h.tt, "Host key changed — possible attack. Remove the old key from known_hosts by hand.") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	if has(h.tt, "Trust") || has(h.tt, "Verify host key…") {
		t.Fatalf("trust offered on mismatch: %q", h.tt.Texts())
	}
}

func TestRowShowsReadoutAndDashesWhenUnknown(t *testing.T) {
	h := newHarness(t, baseConfig())
	h.views["vps"] = hub.HostView{Conn: hub.Conn{State: hub.Online, LastOK: time.Now()},
		Snap: &model.Snapshot{CPU: model.CPU{Total: 12.4}, Memory: model.Memory{Total: 1000, Used: 430}}}
	h.tt.Frame()
	if !has(h.tt, "CPU 12% · RAM 43%") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	if n := strings.Count(strings.Join(h.tt.Texts(), "|"), "CPU — · RAM —"); n != 2 {
		t.Errorf("%d unknown readouts, want 2 (This Mac, NAS): %q", n, h.tt.Texts())
	}
}

func TestRenderPNG(t *testing.T) {
	path := os.Getenv("HOTWATCH_E3_PNG")
	if path == "" {
		t.Skip("set HOTWATCH_E3_PNG to write the panel image")
	}
	h := newHarness(t, baseConfig())
	h.sel = "vps"
	snap := func(cpu float64, used uint64) *model.Snapshot {
		return &model.Snapshot{CPU: model.CPU{Total: cpu}, Memory: model.Memory{Total: 100, Used: used}}
	}
	h.views["local"] = hub.HostView{Conn: hub.Conn{State: hub.Online}, Snap: snap(8, 61)}
	h.views["vps"] = hub.HostView{Conn: hub.Conn{State: hub.Online}, Snap: snap(12, 43)}
	h.views["nas"] = hub.HostView{Conn: hub.Conn{State: hub.HostKeyMismatch}, HostKey: &transport.HostKeyError{Host: "nas.lan", Fingerprint: "SHA256:x", Mismatch: true}}
	h.tt.SetSize(280, 420)
	h.tt.Frame()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, h.tt.Image()); err != nil {
		t.Fatal(err)
	}
}
