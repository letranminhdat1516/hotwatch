//go:build !darwin && !windows

package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"hotwatch/store"
)

// newSecrets keeps host tokens in the desktop's Secret Service (GNOME
// Keyring, KWallet) through secret-tool when a session bus is there, else
// in a 0600 file beside the config.
func newSecrets(p paths) (store.Secrets, error) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
		if s, ok := store.NewSecretTool("Hotwatch"); ok {
			return s, nil
		}
	}
	f := filepath.Join(filepath.Dir(p.Config), "secrets.json")
	slog.Warn("no Secret Service (install libsecret-tools for secret-tool); host tokens are kept in a private file", "path", f)
	return store.NewFileSecrets(f), nil
}
