package main

import (
	"github.com/egoist/mygo"

	"hotwatch/source"
	"hotwatch/source/local"
	"hotwatch/store"
)

func newLocalSource() source.Source { return local.New() }

func newSecrets(paths) (store.Secrets, error) { return store.NewKeychain("Hotwatch") }

// windowChrome hides the title bar; the sidebar draws under the traffic
// lights.
func windowChrome(o *mygo.WindowOptions) {
	o.TitleBarStyle = mygo.TitleBarHiddenInset
	o.TrafficLightPosition = &mygo.Point{X: 14, Y: 14}
}
