package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileConfigRoundTripAndMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	want := FileConfig{Listen: "127.0.0.1:7171", TokenSHA256: SHA256Hex("x"), ReadOnly: true, IntervalMs: 3000}
	if err := WriteConfig(p, want, false); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
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
	os.WriteFile(p, []byte(good+`}`), 0o644)
	os.Chmod(p, 0o644)
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
