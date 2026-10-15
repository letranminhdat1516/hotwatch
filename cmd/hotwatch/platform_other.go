//go:build !darwin

package main

import (
	"errors"

	"hotwatch/source"
	"hotwatch/store"
)

// The app runs on macOS only; elsewhere it builds but has no local host or
// Keychain.

var errNotMac = errors.New("hotwatch runs on macOS")

func newLocalSource() source.Source { return failing{errNotMac} }

func newSecrets() (store.Secrets, error) { return nil, errNotMac }
