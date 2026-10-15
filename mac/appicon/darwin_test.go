//go:build darwin && arm64

package appicon

import (
	"testing"
	"time"
)

func TestDarwinLoadsCalculatorIcon(t *testing.T) {
	done := make(chan func(), 8)
	d := NewDarwin(func(f func()) { done <- f }, 40)
	const calc = "/System/Applications/Calculator.app"
	if _, ok := d.Icon(calc); ok {
		t.Fatal("answered before loading")
	}
	select {
	case f := <-done:
		f() // the main-thread delivery
	case <-time.After(5 * time.Second):
		t.Fatal("icon not delivered in 5 s")
	}
	bm, ok := d.Icon(calc)
	if !ok || bm == nil {
		t.Fatal("Calculator icon missing after delivery")
	}
	if w, h := bm.Size(); w != 40 || h != 40 {
		t.Fatalf("icon is %d×%d, want 40×40", w, h)
	}
	if _, ok := d.Icon("/nonexistent/Nope.app"); ok {
		t.Fatal("a missing bundle answered at once")
	}
}

func TestDarwinServesTheExecutableIcon(t *testing.T) {
	done := make(chan func(), 4)
	d := NewDarwin(func(f func()) { done <- f }, 40)
	d.Icon(Executable)
	select {
	case f := <-done:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("executable icon not delivered")
	}
	if bm, ok := d.Icon(Executable); !ok || bm == nil {
		t.Fatal("no icon for Unix executables")
	}
}
