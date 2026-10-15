package nook

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

func TestMotionOpensWhileHoveredAndClosesAfterTheGrace(t *testing.T) {
	var m Motion
	m.Update(at(0), false)
	if m.Phase() != Idle || m.Progress(at(0)) != 0 || m.Shown() {
		t.Fatalf("start: %v %v", m.Phase(), m.Progress(at(0)))
	}
	m.Update(at(10), true)
	if m.Phase() != Opening || !m.Shown() {
		t.Fatalf("hover: %v", m.Phase())
	}
	if p := m.Progress(at(10 + int(OpenTime.Milliseconds())/2)); p <= 0.5 || p >= 1 {
		t.Fatalf("half-way progress %v: an ease-out is past half at half time", p)
	}
	m.Update(at(10+int(OpenTime.Milliseconds())), true)
	if m.Phase() != Open || m.Progress(at(500)) != 1 {
		t.Fatalf("after open time: %v", m.Phase())
	}
	// Leaving starts the grace; coming back within it keeps it open.
	m.Update(at(1000), false)
	m.Update(at(1000+int(Grace.Milliseconds())-50), true)
	m.Update(at(1600), false)
	if m.Phase() != Open {
		t.Fatalf("closed during the grace: %v", m.Phase())
	}
	m.Update(at(1600+int(Grace.Milliseconds())), false)
	if m.Phase() != Closing {
		t.Fatalf("after the grace: %v", m.Phase())
	}
	end := 1600 + int(Grace.Milliseconds()) + int(CloseTime.Milliseconds())
	if p := m.Progress(at(end - 1)); p <= 0 || p >= 1 {
		t.Fatalf("closing progress %v", p)
	}
	m.Update(at(end), false)
	if m.Phase() != Idle || m.Shown() {
		t.Fatalf("after closing: %v", m.Phase())
	}
}

func TestMotionReopensFromWhereItWasWhileClosing(t *testing.T) {
	var m Motion
	m.Update(at(0), true)
	m.Update(at(int(OpenTime.Milliseconds())), true)
	m.Update(at(1000), false)
	m.Update(at(1000+int(Grace.Milliseconds())), false) // closing starts
	mid := 1000 + int(Grace.Milliseconds()) + int(CloseTime.Milliseconds())/2
	before := m.Progress(at(mid))
	m.Update(at(mid), true)
	if m.Phase() != Opening {
		t.Fatalf("hover while closing: %v", m.Phase())
	}
	if after := m.Progress(at(mid)); after < before-0.05 || after > before+0.05 {
		t.Fatalf("progress jumped from %v to %v", before, after)
	}
}

func TestAlertOpensForFourSecondsWithoutAPointer(t *testing.T) {
	var m Motion
	m.Alert(at(0))
	m.Update(at(0), false)
	if m.Phase() != Opening {
		t.Fatalf("alert: %v", m.Phase())
	}
	m.Update(at(3900), false)
	if m.Phase() != Open {
		t.Fatalf("alert before 4 s: %v", m.Phase())
	}
	m.Update(at(4000+int(Grace.Milliseconds())), false)
	if m.Phase() != Closing {
		t.Fatalf("alert after 4 s and the grace: %v", m.Phase())
	}
}

func TestMotionAnimatingOnlyWhileMoving(t *testing.T) {
	var m Motion
	if m.Animating() {
		t.Fatal("idle animates")
	}
	m.Update(at(0), true)
	if !m.Animating() {
		t.Fatal("opening does not animate")
	}
	m.Update(at(1000), true)
	if m.Animating() {
		t.Fatal("open animates")
	}
}
