package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileSecretsRoundTripAndPrivacy(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Hotwatch", "secrets.json")
	s := NewFileSecrets(p)
	if _, err := s.Get("k"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("missing file: %v", err)
	}
	if err := s.Set("k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("k2", "v2"); err != nil {
		t.Fatal(err)
	}
	if v, err := NewFileSecrets(p).Get("k"); err != nil || v != "v1" {
		t.Fatalf("reopened: %q %v", v, err)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", st.Mode())
		}
	}
	if err := s.Delete("k"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("absent"); err != nil {
		t.Fatalf("deleting a missing key: %v", err)
	}
	if _, err := s.Get("k"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "v1") || !strings.Contains(string(b), "v2") {
		t.Fatalf("file %s", b)
	}
}

// fakeTool records secret-tool calls; stored maps attributes to secrets.
type fakeTool struct {
	stored map[string]string
	calls  []string
}

func (f *fakeTool) run(stdin string, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	key := args[len(args)-1]
	switch args[0] {
	case "store":
		f.stored[key] = stdin
		return "", nil
	case "lookup":
		v, ok := f.stored[key]
		if !ok {
			return "", errSecretToolMissing
		}
		return v, nil
	case "clear":
		delete(f.stored, key)
		return "", nil
	}
	return "", errors.New("bad command")
}

func TestSecretToolPassesSecretsOnStdinOnly(t *testing.T) {
	f := &fakeTool{stored: map[string]string{}}
	s := &SecretTool{service: "Hotwatch", run: f.run}
	if err := s.Set("hotwatch/host/a", "tok"); err != nil {
		t.Fatal(err)
	}
	if v, err := s.Get("hotwatch/host/a"); err != nil || v != "tok" {
		t.Fatalf("get %q %v", v, err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "tok") {
			t.Fatalf("secret on the command line: %q", c)
		}
	}
	if err := s.Delete("hotwatch/host/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("hotwatch/host/a"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}
