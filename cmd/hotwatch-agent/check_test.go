package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"hotwatch/agent"
)

func TestCheckAcceptsAGoodConfigWithoutPrintingSecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hotwatch")
	if err := agent.PrepareConfigDir(dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "agent.json")
	digest := agent.SHA256Hex("tok")
	if err := agent.WriteConfig(p, agent.FileConfig{Listen: "127.0.0.1:7171", TokenSHA256: digest}, false); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdCheck([]string{"--config", p}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "127.0.0.1:7171") || strings.Contains(out.String(), digest) {
		t.Fatalf("output %q", out.String())
	}
	if err := cmdCheck([]string{"--config", filepath.Join(dir, "missing.json")}, &out); err == nil {
		t.Fatal("a missing config must fail")
	}
}
