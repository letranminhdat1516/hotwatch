package pkgs

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseBrewInfo(t *testing.T) {
	src, err := os.ReadFile("testdata/brew-info.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseBrewInfo(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "go", Kind: "formula", Version: "1.27.0", Newest: "1.27.1", Outdated: true, Tap: "homebrew/core"},
		{Name: "node", Kind: "formula", Version: "24.9.0", Newest: "24.10.0", Outdated: true, Tap: "homebrew/core"},
		{Name: "ripgrep", Kind: "formula", Version: "15.1.0", Newest: "15.1.0", Tap: "homebrew/core"},
		{Name: "visual-studio-code", Kind: "cask", Version: "1.49.2", Newest: "1.53.0", Outdated: true,
			Desc: "Open-source code editor", Tap: "homebrew/cask", Homepage: "https://code.visualstudio.com/", Installed: time.Unix(1772276104, 0),
			AutoUpdates: true, Apps: []string{"Visual Studio Code.app"}},
		{Name: "iterm2", Kind: "cask", Version: "3.5.14", Newest: "3.5.14", Tap: "homebrew/cask", Homepage: "https://iterm2.com/"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseMise(t *testing.T) {
	ls, _ := os.ReadFile("testdata/mise-ls.json")
	outdated, _ := os.ReadFile("testdata/mise-outdated.json")
	got, err := parseMise(ls, outdated)
	if err != nil {
		t.Fatal(err)
	}
	want := []Package{
		{Name: "node", Kind: "mise", Version: "24.9.0", Newest: "24.10.0", Outdated: true},
		{Name: "python", Kind: "mise", Version: "3.13.5", Newest: "3.13.5"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestManagerCommandsAreArgvAndKindAware(t *testing.T) {
	var calls [][]string
	m := NewManager(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		switch {
		case name == "brew" && slices.Contains(args, "--json=v2"):
			b, _ := os.ReadFile("testdata/brew-info.json")
			return b, nil
		case name == "mise" && args[0] == "ls":
			b, _ := os.ReadFile("testdata/mise-ls.json")
			return b, nil
		case name == "mise":
			b, _ := os.ReadFile("testdata/mise-outdated.json")
			return b, nil
		}
		return nil, nil
	}, func(ctx context.Context, name string, args ...string) (<-chan string, func() error, error) {
		calls = append(calls, append([]string{name}, args...))
		ch := make(chan string, 1)
		ch <- "ok"
		close(ch)
		return ch, func() error { return nil }, nil
	})
	m.have = map[string]bool{"brew": true, "mise": true}
	list, err := m.List(context.Background())
	if err != nil || len(list) != 7 {
		t.Fatalf("List = %d %v", len(list), err)
	}
	for _, tc := range []struct {
		verb, name string
		want       string
	}{
		{"upgrade", "formula:go", "brew upgrade go"},
		{"upgrade", "cask:visual-studio-code", "brew upgrade --cask visual-studio-code"},
		{"remove", "cask:iterm2", "brew uninstall --cask iterm2"},
		{"upgrade-all", "", "brew upgrade"},
		{"prune", "", "brew cleanup --prune=all"},
		{"upgrade", "mise:node", "mise upgrade node"},
		{"prune", "mise:", "mise prune --yes"},
	} {
		lines, wait, err := m.Run(context.Background(), tc.verb, tc.name)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.verb, tc.name, err)
		}
		for range lines {
		}
		if err := wait(); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(calls[len(calls)-1], " "); got != tc.want {
			t.Errorf("%s %s ran %q, want %q", tc.verb, tc.name, got, tc.want)
		}
	}
	if _, _, err := m.Run(context.Background(), "upgrade", "formula:go; rm -rf /"); err == nil {
		t.Fatal("a name that is not a package token was accepted")
	}
	if _, _, err := m.Run(context.Background(), "format", "formula:go"); err == nil {
		t.Fatal("an unknown verb was accepted")
	}
}
