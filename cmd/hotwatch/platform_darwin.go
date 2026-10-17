package main

import (
	"path/filepath"

	"github.com/egoist/mygo"

	"hotwatch/source"
	"hotwatch/source/local"
	"hotwatch/store"
)

func newLocalSource() source.Source { return local.New() }

// newSecrets keeps host tokens in a 0600 file beside the config, so the
// Keychain never asks. A token an older build left in the Keychain is moved
// into the file the first time it is read.
func newSecrets(p paths) (store.Secrets, error) {
	f := store.NewFileSecrets(filepath.Join(filepath.Dir(p.Config), "secrets.json"))
	kc, err := store.NewKeychain("Hotwatch")
	if err != nil {
		return f, nil
	}
	return store.Migrating{To: f, From: kc}, nil
}

// windowChrome hides the title bar; the sidebar draws under the traffic
// lights.
func windowChrome(o *mygo.WindowOptions) {
	o.TitleBarStyle = mygo.TitleBarHiddenInset
	o.TrafficLightPosition = &mygo.Point{X: 14, Y: 14}
}

// closeBehavior hides the window on close; the Dock icon brings it back.
func closeBehavior(win *mygo.Window) { hideOnClose(win) }
