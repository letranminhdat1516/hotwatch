package main

import "github.com/egoist/mygo"

// mainWindow makes the close button hide the main window, as Mac apps do:
// the nook keeps the app running, so closing would leave no way back. A
// Dock click (or opening the app again) shows it; quitting closes it.
type mainWindow struct {
	hide, show func()
	quitting   bool
}

func (m *mainWindow) onClose(e *mygo.CloseEvent) {
	if m.quitting {
		return
	}
	e.PreventDefault()
	m.hide()
}

func (m *mainWindow) onActivate(bool)              { m.show() }
func (m *mainWindow) onBeforeQuit(*mygo.QuitEvent) { m.quitting = true }

// hideOnClose installs mainWindow on win.
func hideOnClose(win *mygo.Window) {
	m := &mainWindow{hide: win.Hide, show: func() { win.Show(); win.Focus() }}
	win.OnClose(m.onClose)
	mygo.App.OnActivate(m.onActivate)
	mygo.App.OnBeforeQuit(m.onBeforeQuit)
}
