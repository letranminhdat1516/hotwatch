//go:build !darwin

package main

import (
	"github.com/egoist/mygo"

	"hotwatch/source"
	"hotwatch/source/local"
)

// Off macOS the local host is read by the Linux or portable collector; the
// nook, Apps and Packages stay macOS features.

func newLocalSource() source.Source { return local.New() }

// windowChrome keeps the system's title bar: its buttons sit at the right
// on Windows and many Linux desktops, over where the page header draws.
func windowChrome(*mygo.WindowOptions) {}
