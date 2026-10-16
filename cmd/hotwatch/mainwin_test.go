package main

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestCloseHidesUntilQuit(t *testing.T) {
	var hidden, shown int
	m := &mainWindow{hide: func() { hidden++ }, show: func() { shown++ }}

	e := &mygo.CloseEvent{}
	m.onClose(e)
	if !e.DefaultPrevented() || hidden != 1 {
		t.Fatalf("the close button must hide the window: prevented=%v hidden=%d", e.DefaultPrevented(), hidden)
	}
	m.onActivate(false)
	if shown != 1 {
		t.Fatalf("a Dock click must show the window again: shown=%d", shown)
	}

	m.onBeforeQuit(&mygo.QuitEvent{})
	e = &mygo.CloseEvent{}
	m.onClose(e)
	if e.DefaultPrevented() || hidden != 1 {
		t.Fatal("quitting (Cmd+Q) must close the window, not hide it")
	}
}
