package store

import (
	"encoding/json"
	"hotwatch/internal/testenv"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCorruptConfigIsSetAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte("{not json"), 0o600)
	c, warn, err := Load(p)
	if err != nil || warn == "" || c.Hosts[0].ID != "local" {
		t.Fatalf("Load = %+v %q %v", c, warn, err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "config.json.bad-*"))
	if len(matches) != 1 {
		t.Fatalf("bad file not kept: %v", matches)
	}
	if !strings.Contains(string(warn), filepath.Base(matches[0])) {
		t.Fatalf("warning %q does not name %s", warn, matches[0])
	}
	b, _ := os.ReadFile(matches[0])
	if string(b) != "{not json" {
		t.Fatalf("bad file content changed: %q", b)
	}
}

func TestWrongVersionIsCorrupt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte(`{"version":2}`), 0o600)
	c, warn, err := Load(p)
	if err != nil || warn == "" || len(c.Hosts) != 1 {
		t.Fatalf("Load = %+v %q %v", c, warn, err)
	}
}

func TestMissingFileIsDefault(t *testing.T) {
	c, warn, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || warn != "" || c.Hosts[0].ID != "local" || c.Version != 1 {
		t.Fatalf("Load = %+v %q %v", c, warn, err)
	}
}

func TestDefault(t *testing.T) {
	c := Default()
	if c.Version != 1 || c.Groups[0] != (Group{ID: "g-local", Name: "Local"}) ||
		c.Hosts[0].Group != "g-local" || c.Hosts[0].Kind != "local" ||
		c.Settings.IntervalMs != 1000 || c.Settings.Language != "en" ||
		c.UI.SelectedHost != "local" || c.UI.Page != "overview" || c.UI.OpenGroups[0] != "g-local" {
		t.Fatalf("Default = %+v", c)
	}
}

func TestRemoveGroupMovesHostsToParent(t *testing.T) {
	c := Default()
	c.Groups = append(c.Groups, Group{ID: "g-servers", Name: "Servers"}, Group{ID: "g-eu", Name: "EU", Parent: "g-servers"})
	c.Hosts = append(c.Hosts, Host{ID: "h1", Name: "VPS", Kind: "agent", Group: "g-eu"}, Host{ID: "h2", Name: "VPS", Kind: "agent", Group: "g-eu"})
	c.RemoveGroup("g-eu")
	for _, h := range c.Hosts[1:] {
		if h.Group != "g-servers" {
			t.Fatalf("host %s in %q, want g-servers", h.ID, h.Group)
		}
	}
	if len(c.Hosts) != 3 {
		t.Fatalf("hosts lost: %+v", c.Hosts)
	}
	for _, g := range c.Groups {
		if g.ID == "g-eu" {
			t.Fatal("group not removed")
		}
	}
}

func TestRemoveTopLevelGroupUngroupsHostsAndChildren(t *testing.T) {
	c := Default()
	c.Groups = append(c.Groups, Group{ID: "g-s", Name: "S"}, Group{ID: "g-c", Name: "C", Parent: "g-s"})
	c.Hosts = append(c.Hosts, Host{ID: "h1", Name: "A", Kind: "agent", Group: "g-s"})
	c.RemoveGroup("g-s")
	if c.Hosts[1].Group != "" {
		t.Fatalf("host group = %q", c.Hosts[1].Group)
	}
	for _, g := range c.Groups {
		if g.ID == "g-c" && g.Parent != "" {
			t.Fatalf("child parent = %q", g.Parent)
		}
	}
	c.RemoveGroup("g-local")
	if c.Hosts[0].Group != "" {
		t.Fatalf("local host group = %q", c.Hosts[0].Group)
	}
	c.RemoveGroup("missing") // no-op
}

func TestMoveHost(t *testing.T) {
	c := Default()
	c.Groups = append(c.Groups, Group{ID: "g-x", Name: "X"})
	if err := c.MoveHost("local", "g-x"); err != nil || c.Hosts[0].Group != "g-x" {
		t.Fatalf("move: %v %+v", err, c.Hosts[0])
	}
	if err := c.MoveHost("local", ""); err != nil || c.Hosts[0].Group != "" {
		t.Fatalf("move to root: %v", err)
	}
	if err := c.MoveHost("nope", "g-x"); err == nil {
		t.Fatal("unknown host accepted")
	}
	if err := c.MoveHost("local", "g-nope"); err == nil {
		t.Fatal("unknown group accepted")
	}
	if c.Hosts[0].Group != "" {
		t.Fatalf("failed move mutated host: %+v", c.Hosts[0])
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	c := Default()
	c.Settings.IntervalMs = 5000
	c.Hosts = append(c.Hosts, Host{ID: "h", Name: "B", Kind: "agent", Group: "g-local",
		Conn: &Conn{Type: "ssh", SSHHost: "1.2.3.4", SSHPort: 22, SSHUser: "root", AgentPort: 7171}})
	c.Settings.Columns = map[string][]string{"processes": {"name", "pid"}}
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, warn, err := Load(p)
	if err != nil || warn != "" || got.Settings.IntervalMs != 5000 || got.Hosts[1].Conn.SSHHost != "1.2.3.4" ||
		len(got.Settings.Columns["processes"]) != 2 {
		t.Fatalf("got %+v %q %v", got, warn, err)
	}
	st, _ := os.Stat(p)
	if testenv.UnixModes() && st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v", st.Mode())
	}
	ds, _ := os.Stat(filepath.Dir(p))
	if testenv.UnixModes() && ds.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", ds.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("leftover temp files: %v", entries)
	}
}

func TestJSONKeys(t *testing.T) {
	c := Default()
	c.Hosts = append(c.Hosts, Host{ID: "d", Name: "D", Kind: "agent", Conn: &Conn{Type: "direct", Address: "1.1.1.1", AgentPort: 7171}})
	b, _ := json.Marshal(c)
	for _, k := range []string{`"version"`, `"groups"`, `"hosts"`, `"settings"`, `"ui"`, `"id"`, `"name"`, `"kind"`, `"group"`,
		`"conn"`, `"type"`, `"address"`, `"agentPort"`, `"intervalMs"`, `"language"`, `"launchAtLogin"`, `"selectedHost"`, `"page"`, `"openGroups"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("missing key %s in %s", k, b)
		}
	}
	var r Config
	if err := json.Unmarshal([]byte(`{"version":1,"hosts":[{"id":"h","name":"n","kind":"agent","group":"g","conn":{"type":"ssh","sshHost":"a","sshPort":22,"sshUser":"u","keyPath":"k","agentPort":1}}],"groups":[{"id":"g","name":"G","parent":"p"}]}`), &r); err != nil ||
		r.Hosts[0].Conn.KeyPath != "k" || r.Groups[0].Parent != "p" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestMemSecrets(t *testing.T) {
	var s Secrets = NewMemSecrets()
	if _, err := s.Get(TokenKey("h")); err == nil {
		t.Fatal("expected not-found error")
	}
	if TokenKey("h") != "hotwatch/host/h" {
		t.Fatal(TokenKey("h"))
	}
	if err := s.Set(TokenKey("h"), "tok"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get(TokenKey("h")); err != nil || v != "tok" {
		t.Fatalf("%q %v", v, err)
	}
	if err := s.Delete(TokenKey("h")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(TokenKey("h")); err == nil {
		t.Fatal("still present")
	}
	if err := s.Delete(TokenKey("h")); err != nil {
		t.Fatalf("delete of missing key should be nil: %v", err)
	}
}

func TestLocalHostNamePerOS(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "This Mac", "windows": "This PC", "linux": "This Computer", "freebsd": "This Computer"} {
		if got := localHostName(goos); got != want {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
	if Default().Hosts[0].Name != localHostName(runtime.GOOS) {
		t.Fatalf("default host %q", Default().Hosts[0].Name)
	}
}
