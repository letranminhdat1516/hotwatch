// Package apps lists the applications installed on the Mac and moves one to
// the Trash. The darwin scanner lands with the Apps page; the contract and
// the Fake live here so the page can be built against them.
package apps

import (
	"context"
	"time"
)

// App is one installed application bundle.
type App struct {
	Name, Bundle, BundleID, Version string
	Size                            uint64    // 0: not computed yet
	Arch                            string    // "Universal", "Apple silicon", "Intel", ""
	Source                          string    // "App Store", "Homebrew", ""
	LastOpened                      time.Time // zero: never / unknown
}

// Scanner finds applications. Apps is fast and returns no sizes; Size walks
// one bundle and is called off the main thread for visible rows only, the
// caller caching the result; Uninstall moves the bundle to the Trash through
// Finder, which asks for administrator rights itself when the bundle is not
// writable by the user.
type Scanner interface {
	Apps(ctx context.Context) ([]App, error)
	Size(ctx context.Context, bundle string) (uint64, error)
	Uninstall(ctx context.Context, bundle string) error
}
