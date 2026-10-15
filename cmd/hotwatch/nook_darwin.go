//go:build darwin

package main

import "github.com/ebitengine/purego/objc"

// NSStatusWindowLevel puts the nook over the menu bar, where the notch is;
// the behaviours keep it on every Space, out of Exposé and window cycling.
const (
	nsStatusWindowLevel = 25
	canJoinAllSpaces    = 1 << 0
	stationary          = 1 << 4
	ignoresCycle        = 1 << 6
)

var (
	selSetLevel    = objc.RegisterName("setLevel:")
	selSetBehavior = objc.RegisterName("setCollectionBehavior:")
)

func overMenuBar(handle uintptr) {
	if handle == 0 {
		return
	}
	w := objc.ID(handle)
	w.Send(selSetLevel, nsStatusWindowLevel)
	w.Send(selSetBehavior, canJoinAllSpaces|stationary|ignoresCycle)
}
