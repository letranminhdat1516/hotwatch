//go:build !windows

package agent

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func ownerOf(t *testing.T, p string) [2]uint32 {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	s := st.Sys().(*syscall.Stat_t)
	return [2]uint32{s.Uid, s.Gid}
}

func assertPrivate(t *testing.T, p string) {
	t.Helper()
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
}

func makePublic(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteConfigReplacePreservesOwner(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	c := FileConfig{Listen: "127.0.0.1:7171", TokenSHA256: SHA256Hex("a"), IntervalMs: 2000}
	if err := WriteConfig(p, c, false); err != nil {
		t.Fatal(err)
	}
	before := ownerOf(t, p)
	c.TokenSHA256 = SHA256Hex("b")
	if err := WriteConfig(p, c, true); err != nil {
		t.Fatal(err)
	}
	if after := ownerOf(t, p); after != before {
		t.Fatalf("owner changed %v -> %v", before, after)
	}
	assertPrivate(t, p)
}
