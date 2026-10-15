package main

import (
	"testing"
	"time"

	"hotwatch/hub"
	"hotwatch/mac/notch"
	"hotwatch/store"
	"hotwatch/ui/nook"
)

type fakeWin struct {
	bounds  notch.Rect
	shown   bool
	invalid int
}

func (w *fakeWin) place(r notch.Rect) { w.bounds = r }
func (w *fakeWin) show()              { w.shown = true }
func (w *fakeWin) hide()              { w.shown = false }
func (w *fakeWin) redraw()            { w.invalid++ }

type fakeNookApp struct {
	selected string
	views    map[string]hub.HostView
}

func (a *fakeNookApp) Selected() string                { return a.selected }
func (a *fakeNookApp) HostView(id string) hub.HostView { return a.views[id] }
func (a *fakeNookApp) Hosts() []store.Host {
	return []store.Host{{ID: "local", Name: "This Mac"}, {ID: "vps", Name: "VPS"}}
}
func (a *fakeNookApp) Paused() bool { return false }

var geo = notch.Geometry{Screen: notch.Rect{W: 1512, H: 982}, Notch: notch.Rect{X: 665, W: 185, H: 32}}

func newNook(t *testing.T) (*nookCtl, *fakeWin, *fakeWin) {
	bar, island := &fakeWin{}, &fakeWin{}
	n := newNookCtl(notch.Fake{Geometry: geo, OK: true}, bar, island, &fakeNookApp{selected: "local", views: map[string]hub.HostView{}})
	n.relayout()
	return n, bar, island
}

var n0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func TestNookPlacesTheBarAndIslandAroundTheNotch(t *testing.T) {
	_, bar, island := newNook(t)
	if bar.bounds != notch.Ears(geo.Notch, nook.EarW, nook.Shoulder) || !bar.shown {
		t.Fatalf("bar %+v shown %v", bar.bounds, bar.shown)
	}
	if island.bounds != notch.PanelRect(geo.Notch, nook.PanelW, nook.PanelH) || island.shown {
		t.Fatalf("island %+v shown %v", island.bounds, island.shown)
	}
}

func TestNookOpensOnHoverAndHidesAfterClosing(t *testing.T) {
	n, _, island := newNook(t)
	n.tick(n0, 100, 500) // far away
	if island.shown {
		t.Fatal("shown without a pointer")
	}
	n.tick(n0.Add(70*time.Millisecond), 757, 20) // on the notch
	if !island.shown || island.invalid == 0 {
		t.Fatalf("not shown on hover: %+v", island)
	}
	n.tick(n0.Add(600*time.Millisecond), 600, 150) // inside the island, outside the notch zone
	if n.motion.Phase() != nook.Open {
		t.Fatalf("closed while the pointer is on the island: %v", n.motion.Phase())
	}
	n.tick(n0.Add(time.Second), 100, 600)
	closeAt := n0.Add(time.Second + nook.Grace + 10*time.Millisecond) // the first tick after the grace
	n.tick(closeAt, 100, 600)
	n.tick(closeAt.Add(nook.CloseTime+20*time.Millisecond), 100, 600)
	if island.shown {
		t.Fatalf("still shown after closing: %v", n.motion.Phase())
	}
}

func TestNookAlertsWhenAHostGoesOffline(t *testing.T) {
	n, _, island := newNook(t)
	n.observe("vps", hub.Online, n0)
	n.observe("vps", hub.Online, n0.Add(time.Second))
	n.tick(n0.Add(time.Second), 100, 600)
	if island.shown {
		t.Fatal("shown with every host online")
	}
	n.observe("vps", hub.Offline, n0.Add(2*time.Second))
	n.tick(n0.Add(2*time.Second), 100, 600)
	if !island.shown || n.alertHost != "vps" {
		t.Fatalf("no alert for the VPS going offline: shown %v host %q", island.shown, n.alertHost)
	}
	// The first state seen is not a change: starting offline does not alert.
	m, _, island2 := newNook(t)
	m.observe("vps", hub.Offline, n0)
	m.tick(n0, 100, 600)
	if island2.shown {
		t.Fatal("alerted on the first state")
	}
}

func TestNookWithoutANotchStaysHidden(t *testing.T) {
	bar, island := &fakeWin{}, &fakeWin{}
	n := newNookCtl(notch.Fake{}, bar, island, &fakeNookApp{selected: "local"})
	n.relayout()
	n.tick(n0, 757, 20)
	if bar.shown || island.shown {
		t.Fatal("shown without a notch")
	}
}

func TestNookTurnedOffHidesEverything(t *testing.T) {
	n, bar, island := newNook(t)
	n.setOff(true)
	n.tick(n0, 757, 20)
	if bar.shown || island.shown {
		t.Fatal("shown while turned off")
	}
	n.setOff(false)
	if !bar.shown {
		t.Fatal("bar not back after turning on")
	}
}
