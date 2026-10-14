// Package store persists the app configuration (config.json, version 1).
// Tokens are never stored here; see Secrets.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Warning is non-empty when Load had to set a corrupt file aside.
type Warning string

// Conn describes how to reach an agent host. Type is "direct" or "ssh".
type Conn struct {
	Type      string `json:"type"`
	Address   string `json:"address,omitempty"`
	AgentPort int    `json:"agentPort,omitempty"`
	SSHHost   string `json:"sshHost,omitempty"`
	SSHPort   int    `json:"sshPort,omitempty"`
	SSHUser   string `json:"sshUser,omitempty"`
	KeyPath   string `json:"keyPath,omitempty"`
}

// Host is a monitored machine. Kind is "local" or "agent".
type Host struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Group string `json:"group"`
	Conn  *Conn  `json:"conn,omitempty"`
}

// Group organises hosts; Parent "" means top level.
type Group struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
}

type Settings struct {
	IntervalMs    int                 `json:"intervalMs"`
	Language      string              `json:"language"`
	Columns       map[string][]string `json:"columns,omitempty"`
	LaunchAtLogin bool                `json:"launchAtLogin"`
}

type UIState struct {
	SelectedHost string   `json:"selectedHost"`
	Page         string   `json:"page"`
	OpenGroups   []string `json:"openGroups"`
}

type Config struct {
	Version  int      `json:"version"`
	Groups   []Group  `json:"groups"`
	Hosts    []Host   `json:"hosts"`
	Settings Settings `json:"settings"`
	UI       UIState  `json:"ui"`
}

// Default is the first-run configuration: this Mac in group "Local".
func Default() Config {
	return Config{
		Version:  1,
		Groups:   []Group{{ID: "g-local", Name: "Local"}},
		Hosts:    []Host{{ID: "local", Name: "This Mac", Kind: "local", Group: "g-local"}},
		Settings: Settings{IntervalMs: 2000, Language: "en"},
		UI:       UIState{SelectedHost: "local", Page: "overview", OpenGroups: []string{"g-local"}},
	}
}

// Load reads path. A missing file yields Default() with no warning. A file that
// is unparsable or has Version != 1 is renamed to <path>.bad-<UTC stamp> and
// Default() is returned with a Warning naming it.
func Load(path string) (Config, Warning, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), "", nil
	}
	if err != nil {
		return Config{}, "", err
	}
	var c Config
	if jerr := json.Unmarshal(b, &c); jerr != nil || c.Version != 1 {
		reason := "unsupported version"
		if jerr != nil {
			reason = jerr.Error()
		}
		bad := path + ".bad-" + time.Now().UTC().Format("20060102T150405Z")
		if rerr := os.Rename(path, bad); rerr != nil {
			return Config{}, "", fmt.Errorf("set aside corrupt config: %w", rerr)
		}
		return Default(), Warning(fmt.Sprintf("config was unreadable (%s); kept as %s and started with defaults", reason, filepath.Base(bad))), nil
	}
	return c, "", nil
}

// Save writes c atomically: temp file in the same directory, fsync, rename.
func Save(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() { f.Close(); os.Remove(tmp) }
	if err := f.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := f.Write(b); err != nil {
		cleanup()
		return err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
