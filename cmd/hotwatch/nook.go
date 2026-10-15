package main

import (
	"time"

	"hotwatch/hub"
	"hotwatch/mac/notch"
	"hotwatch/store"
	"hotwatch/ui/nook"
)

// nookWin is what the controller does with each of the nook's windows.
type nookWin interface {
	place(r notch.Rect)
	show()
	hide()
	redraw()
}

// nookApp is what the nook reads of the app.
type nookApp interface {
	Selected() string
	HostView(id string) hub.HostView
	Hosts() []store.Host
	Paused() bool
}

// nookCtl runs the nook: it places the bar and the island around the
// notch, opens the island while the pointer is on it or a host just went
// offline, and hides both without a notch or while turned off. Main thread.
type nookCtl struct {
	det    notch.Detector
	bar    nookWin
	island nookWin
	app    nookApp

	geo     notch.Geometry
	has     bool // a notch was found
	off     bool // turned off in Settings
	motion  nook.Motion
	shown   bool // the island window is on screen
	states  map[string]hub.ConnState
	entry   notch.Rect
	stay    notch.Rect
	panel   notch.Rect
	hover   bool
	lastNow time.Time

	alertHost string // the host whose going offline opened the island
}

func newNookCtl(det notch.Detector, bar, island nookWin, app nookApp) *nookCtl {
	return &nookCtl{det: det, bar: bar, island: island, app: app, states: map[string]hub.ConnState{}}
}

// relayout finds the notch again (at start and when the screens change)
// and places the windows.
func (n *nookCtl) relayout() {
	n.geo, n.has = n.det.Detect()
	if !n.has {
		n.bar.hide()
		n.hideIsland()
		return
	}
	n.panel = notch.PanelRect(n.geo.Notch, nook.PanelW, nook.PanelH)
	n.entry, n.stay = notch.HoverZones(n.geo.Notch, n.panel)
	n.bar.place(notch.Ears(n.geo.Notch, nook.EarW, nook.Shoulder))
	n.island.place(n.panel)
	if !n.off {
		n.bar.show()
	}
}

// setOff turns the nook off or back on.
func (n *nookCtl) setOff(off bool) {
	n.off = off
	if off {
		n.bar.hide()
		n.hideIsland()
		n.motion = nook.Motion{}
		return
	}
	if n.has {
		n.bar.show()
	}
}

func (n *nookCtl) hideIsland() {
	if n.shown {
		n.island.hide()
		n.shown = false
	}
}

// tick takes the pointer at x, y (screen DIPs) at now: the island opens
// while the pointer is in the entry zone (or the island, once open) and
// closes after it leaves.
func (n *nookCtl) tick(now time.Time, x, y float64) {
	n.lastNow = now
	if !n.has || n.off {
		return
	}
	zone := n.entry
	if n.motion.Shown() {
		zone = n.stay
	}
	n.hover = zone.Contains(x, y)
	n.motion.Update(now, n.hover)
	switch {
	case n.motion.Shown() && !n.shown:
		n.island.show()
		n.shown = true
		n.island.redraw()
	case !n.motion.Shown() && n.shown:
		n.hideIsland()
		n.alertHost = ""
	case n.motion.Animating():
		n.island.redraw()
	}
}

// observe takes a host's connection state at now: a host that was online
// and is now offline opens the island for nook.AlertTime. The first state
// seen of a host is not a change.
func (n *nookCtl) observe(id string, state hub.ConnState, now time.Time) {
	prev, seen := n.states[id]
	n.states[id] = state
	if !seen || prev != hub.Online || state != hub.Offline || n.off || !n.has {
		return
	}
	n.alertHost = id
	n.motion.Alert(now)
}

// islandInput is what the island draws at now.
func (n *nookCtl) islandInput(now time.Time, onHost func(string), onPage func(string), onOpen, onPause func()) nook.IslandInput {
	sel := n.app.Selected()
	var chips []nook.Chip
	for _, h := range n.app.Hosts() {
		state := n.app.HostView(h.ID).Conn.State
		if h.ID == "local" {
			state = hub.Online
		}
		chips = append(chips, nook.Chip{ID: h.ID, Name: h.Name, State: state})
	}
	return nook.IslandInput{
		Progress: n.motion.Progress(now), NotchW: float32(n.geo.Notch.W), NotchH: float32(n.geo.Notch.H),
		Hosts: chips, Selected: sel, View: n.app.HostView(sel), Now: now, Paused: n.app.Paused(),
		OnHost: onHost, OnPage: onPage, OnOpen: onOpen, OnPause: onPause,
	}
}
