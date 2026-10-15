//go:build darwin && arm64

package notch

import (
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// System finds the notch through AppKit's NSScreen: the screen whose
// safe-area insets have a top (only a notched built-in display does), and
// its auxiliary menu-bar areas left and right of the notch. arm64-only:
// the NSRect and NSEdgeInsets it reads come back in float registers there.
type System struct{}

var _ Detector = System{}

type nsRect struct{ X, Y, W, H float64 }
type nsEdgeInsets struct{ Top, Left, Bottom, Right float64 }

var appKit = sync.OnceValue(func() error {
	_, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	return err
})

var (
	selScreens   = objc.RegisterName("screens")
	selCount     = objc.RegisterName("count")
	selObjectAt  = objc.RegisterName("objectAtIndex:")
	selFrame     = objc.RegisterName("frame")
	selSafeArea  = objc.RegisterName("safeAreaInsets")
	selAuxLeft   = objc.RegisterName("auxiliaryTopLeftArea")
	selAuxRight  = objc.RegisterName("auxiliaryTopRightArea")
	selResponds  = objc.RegisterName("respondsToSelector:")
	selAlloc     = objc.RegisterName("alloc")
	selInit      = objc.RegisterName("init")
	selDrain     = objc.RegisterName("drain")
	selSharedApp = objc.RegisterName("sharedApplication")
)

// Detect reads the screens now: call it again when the screens change (a
// lid closes, a display is plugged in).
func (System) Detect() (Geometry, bool) {
	if appKit() != nil {
		return Geometry{}, false
	}
	// An autorelease pool must be drained on the thread that made it: keep
	// this goroutine on one OS thread until it is.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)
	objc.ID(objc.GetClass("NSApplication")).Send(selSharedApp) // NSScreen needs the app object

	screens := objc.ID(objc.GetClass("NSScreen")).Send(selScreens)
	n := int(screens.Send(selCount))
	if n == 0 {
		return Geometry{}, false
	}
	// Cocoa's origin is the bottom-left of the primary (first) screen;
	// flip y against its height.
	primary := objc.Send[nsRect](screens.Send(selObjectAt, 0), selFrame)
	flip := func(r nsRect) Rect { return Rect{X: r.X, Y: primary.H - (r.Y + r.H), W: r.W, H: r.H} }
	for i := 0; i < n; i++ {
		s := screens.Send(selObjectAt, i)
		if s.Send(selResponds, selSafeArea) == 0 {
			continue // before macOS 12: no notch API
		}
		insets := objc.Send[nsEdgeInsets](s, selSafeArea)
		if insets.Top <= 0 {
			continue
		}
		screen := flip(objc.Send[nsRect](s, selFrame))
		var left, right float64
		if s.Send(selResponds, selAuxLeft) != 0 {
			l := objc.Send[nsRect](s, selAuxLeft)
			r := objc.Send[nsRect](s, selAuxRight)
			if l.W > 0 && r.W > 0 {
				left, right = l.X+l.W, r.X
			}
		}
		if nr, ok := NotchRect(screen, insets.Top, left, right); ok {
			return Geometry{Screen: screen, Notch: nr}, true
		}
	}
	return Geometry{}, false
}
