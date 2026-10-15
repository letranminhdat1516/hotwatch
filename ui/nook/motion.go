// Package nook draws hotwatch in the MacBook's notch: a 2 DIP bar under it
// while idle, and an island that grows out of it while the pointer is on
// it. Motion is the island's state machine; Bar and Island are its views.
// Everything here runs on the main thread.
package nook

import (
	"math"
	"time"
)

// Phase is where the island is in its life.
type Phase int

const (
	Idle Phase = iota
	Opening
	Open
	Closing
)

// Timings of the island (UI-DESIGN.md "Nook").
const (
	OpenTime   = 420 * time.Millisecond
	CloseTime  = 300 * time.Millisecond
	Grace      = 450 * time.Millisecond // the pointer may leave this long before it closes
	AlertTime  = 4 * time.Second        // how long an alert keeps it open
	frameDelay = 16 * time.Millisecond
)

// Motion opens the island while it is hovered (or an alert holds it) and
// closes it a grace period after both stop. Progress is 0 closed, 1 open.
type Motion struct {
	phase      Phase
	start      time.Time // when the current Opening or Closing began
	from       float64   // progress at start
	leftAt     time.Time // when the pointer left, zero while it is on it
	alertUntil time.Time
	lastHover  time.Time // last Update with the pointer on the island
}

// Phase is the current phase.
func (m *Motion) Phase() Phase { return m.phase }

// Shown reports whether the island window should be on screen.
func (m *Motion) Shown() bool { return m.phase != Idle }

// Animating reports whether the island is growing or shrinking, so the
// caller keeps drawing frames.
func (m *Motion) Animating() bool { return m.phase == Opening || m.phase == Closing }

// FrameDelay is how often to redraw while Animating.
func FrameDelay() time.Duration { return frameDelay }

// Alert holds the island open for AlertTime from now, pointer or not.
func (m *Motion) Alert(now time.Time) { m.alertUntil = now.Add(AlertTime) }

// Update advances the machine to now with the pointer on the island or not.
func (m *Motion) Update(now time.Time, hovering bool) {
	want := hovering || now.Before(m.alertUntil)
	if hovering {
		m.lastHover = now
	}
	if want {
		m.leftAt = time.Time{}
	} else if m.leftAt.IsZero() {
		// The grace runs from the moment the island stopped being wanted:
		// an alert that ran out between two updates ran out at alertUntil.
		m.leftAt = now
		if m.alertUntil.After(m.lastHover) && m.alertUntil.Before(now) {
			m.leftAt = m.alertUntil
		}
	}
	switch m.phase {
	case Idle:
		if want {
			m.begin(Opening, now, 0)
		}
	case Opening:
		if now.Sub(m.start) >= m.span(OpenTime) {
			m.phase = Open
		}
	case Open:
		if !want && now.Sub(m.leftAt) >= Grace {
			m.begin(Closing, now, 1)
		}
	case Closing:
		if want {
			m.begin(Opening, now, m.Progress(now))
		} else if now.Sub(m.start) >= m.span(CloseTime) {
			m.phase = Idle
		}
	}
}

// begin starts phase at progress from: a reversal mid-way covers only the
// rest of the distance, so the island never jumps.
func (m *Motion) begin(p Phase, now time.Time, from float64) {
	m.phase, m.start, m.from = p, now, from
}

// span is the time the current move takes: the full time scaled by the
// distance it has left to cover.
func (m *Motion) span(full time.Duration) time.Duration {
	dist := 1 - m.from
	if m.phase == Closing {
		dist = m.from
	}
	return time.Duration(float64(full) * math.Max(dist, 0.05))
}

// Progress is how open the island is at now, eased (fast start, soft end).
func (m *Motion) Progress(now time.Time) float64 {
	switch m.phase {
	case Idle:
		return 0
	case Open:
		return 1
	case Opening:
		t := clamp(float64(now.Sub(m.start)) / float64(m.span(OpenTime)))
		return m.from + (1-m.from)*easeOut(t)
	default: // Closing
		t := clamp(float64(now.Sub(m.start)) / float64(m.span(CloseTime)))
		return m.from * (1 - easeOut(t))
	}
}

func easeOut(t float64) float64 { return 1 - math.Pow(1-t, 3) }

func clamp(v float64) float64 { return math.Min(math.Max(v, 0), 1) }
