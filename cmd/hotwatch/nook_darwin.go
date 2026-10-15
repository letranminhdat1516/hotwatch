//go:build darwin

package main

import "github.com/ebitengine/purego/objc"

// NSStatusWindowLevel puts the nook over the menu bar, where the notch is;
// the behaviours keep it on every Space, out of Exposé and window cycling.
const (
	nsStatusWindowLevel         = 25
	nsWindowStyleMaskBorderless = 0
	canJoinAllSpaces            = 1 << 0
	stationary                  = 1 << 4
	ignoresCycle                = 1 << 6
)

var (
	selSetLevel     = objc.RegisterName("setLevel:")
	selSetStyleMask = objc.RegisterName("setStyleMask:")
	selSetBehavior  = objc.RegisterName("setCollectionBehavior:")
)

func overMenuBar(handle uintptr) {
	if handle == 0 {
		return
	}
	w := objc.ID(handle)
	// A titled window (mygo's frameless windows keep a hidden title bar)
	// is held below the menu bar; a borderless one may sit in it, beside
	// the notch.
	w.Send(selSetStyleMask, nsWindowStyleMaskBorderless)
	w.Send(selSetLevel, nsStatusWindowLevel)
	w.Send(selSetBehavior, canJoinAllSpaces|stationary|ignoresCycle)
}
