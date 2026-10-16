package agent

import (
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hotwatch/collect"
	"hotwatch/docker"
)

func TestFileConfigRoundTripAndMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	want := FileConfig{Listen: "127.0.0.1:7171", TokenSHA256: SHA256Hex("x"), ReadOnly: true, IntervalMs: 3000}
	if err := WriteConfig(p, want, false); err != nil {
		t.Fatal(err)
	}
	assertPrivate(t, p)
	got, err := LoadConfig(p)
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := WriteConfig(p, want, false); err == nil {
		t.Fatal("overwrite without replace must fail")
	}
	if err := WriteConfig(p, want, true); err != nil {
		t.Fatalf("replace: %v", err)
	}
}

func TestLoadConfigRejectsBadToken(t *testing.T) {
	for name, digest := range map[string]string{
		"missing": "", "short": "abcd", "nonhex": strings.Repeat("g", 64),
		"long": strings.Repeat("a", 65), "upper": strings.Repeat("A", 64),
	} {
		p := filepath.Join(t.TempDir(), "agent.json")
		os.WriteFile(p, []byte(`{"listen":"127.0.0.1:7171","tokenSHA256":"`+digest+`"}`), 0o600)
		if _, err := LoadConfig(p); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestLoadConfigRejectsUnknownFieldsAndOpenPermissions(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "agent.json")
	good := `{"listen":"127.0.0.1:7171","tokenSHA256":"` + strings.Repeat("a", 64) + `"`
	os.WriteFile(p, []byte(good+`,"token":"plain"}`), 0o600)
	if _, err := LoadConfig(p); err == nil {
		t.Error("unknown field (e.g. a plain token) must be rejected")
	}
	os.WriteFile(p, []byte(good+`}`), 0o600)
	makePublic(t, p)
	if _, err := LoadConfig(p); err == nil {
		t.Error("group/world-readable config must be rejected")
	}
}

func TestNewTokenIs32RandomBytes(t *testing.T) {
	a, b := NewToken(), NewToken()
	if a == b || len(a) != 43 { // 32 bytes base64url without padding
		t.Fatalf("tokens %q %q", a, b)
	}
}

func TestLoadConfigIntervalBounds(t *testing.T) {
	for ms, ok := range map[int]bool{499: false, 500: true, 30000: true, 30001: false, -1: false, 0: true} {
		p := filepath.Join(t.TempDir(), "agent.json")
		os.WriteFile(p, []byte(fmt.Sprintf(`{"listen":"127.0.0.1:7171","tokenSHA256":"%s","intervalMs":%d}`, strings.Repeat("a", 64), ms)), 0o600)
		_, err := LoadConfig(p)
		if (err == nil) != ok {
			t.Errorf("intervalMs %d: err=%v", ms, err)
		} else if err != nil && !strings.Contains(err.Error(), "intervalMs") {
			t.Errorf("error should name the field: %v", err)
		}
	}
}

func writeCfg(t *testing.T, p, digest string, mtime time.Time) {
	t.Helper()
	os.WriteFile(p, []byte(`{"listen":"127.0.0.1:7171","tokenSHA256":"`+digest+`"}`), 0o600)
	os.Chtimes(p, mtime, mtime)
}

func TestTokenReloaderRevokesOldTokenWithinWindow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeCfg(t, p, SHA256Hex("old"), t0)
	clk := newFakeClock()
	var logs []string
	r := NewTokenReloader(p, SHA256Hex("old"), clk, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	srv := httptest.NewServer(New(Config{Token: r.Digest}, &collect.Fake{}, &docker.FakeEngineAPI{}, &FakeSignaller{}, io.Discard))
	defer srv.Close()
	if res := do(t, srv, "GET", "/v1/info", "old", ""); res.StatusCode != 200 {
		t.Fatalf("old token status %d", res.StatusCode)
	}
	writeCfg(t, p, SHA256Hex("new"), t0.Add(time.Hour)) // rotation
	clk.Advance(2 * time.Second)
	if res := do(t, srv, "GET", "/v1/info", "old", ""); res.StatusCode != 200 {
		t.Fatal("reload checked before the 5 s interval")
	}
	clk.Advance(4 * time.Second)
	if res := do(t, srv, "GET", "/v1/info", "old", ""); res.StatusCode != 401 {
		t.Fatalf("old token still valid after window: %d", res.StatusCode)
	}
	if res := do(t, srv, "GET", "/v1/info", "new", ""); res.StatusCode != 200 {
		t.Fatalf("new token status %d", res.StatusCode)
	}
	_ = logs
}

func TestTokenReloaderKeepsHashOnInvalidReplacement(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeCfg(t, p, SHA256Hex("old"), t0)
	clk := newFakeClock()
	var logs []string
	r := NewTokenReloader(p, SHA256Hex("old"), clk, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	for i, bad := range []string{"", "nothex", strings.Repeat("g", 64)} {
		writeCfg(t, p, bad, t0.Add(time.Duration(i+1)*time.Hour))
		clk.Advance(6 * time.Second)
		if got := r.Digest(); got != SHA256Hex("old") {
			t.Fatalf("invalid config %q replaced digest with %q", bad, got)
		}
	}
	os.Remove(p)
	clk.Advance(6 * time.Second)
	if r.Digest() != SHA256Hex("old") {
		t.Fatal("missing file must keep the previous hash")
	}
	if len(logs) == 0 {
		t.Fatal("failures must be logged")
	}
}
