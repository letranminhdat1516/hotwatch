package ui

import "time"

// SetClock replaces the App's clock for tests.
func SetClock(a *App, now func() time.Time) { a.now = now }
