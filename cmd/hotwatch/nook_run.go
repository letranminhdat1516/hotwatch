package main

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hotwatch/mac/notch"
	hwui "hotwatch/ui"
	"hotwatch/ui/nook"
)

// pollEvery is how often the nook reads the pointer: 15 Hz is quick enough
// for a hover and costs well under 1 % of a core.
const pollEvery = 66 * time.Millisecond

// relayoutEvery is how often it looks for the notch again (a lid closed, a
// display plugged in).
const relayoutEvery = 3 * time.Second

// mygoWin is a nook window on mygo.
type mygoWin struct{ w *mygo.Window }

func (m mygoWin) place(r notch.Rect) {
	m.w.SetBounds(mygo.Rectangle{X: int(math.Round(r.X)), Y: int(math.Round(r.Y)), Width: int(math.Round(r.W)), Height: int(math.Round(r.H))})
}
func (m mygoWin) show()   { m.w.ShowInactive() }
func (m mygoWin) hide()   { m.w.Hide() }
func (m mygoWin) redraw() { m.w.Invalidate() }

// startNook creates the bar and island windows over the notch and starts
// reading the pointer. Main thread, after the main window exists.
func startNook(c *core, app *hwui.App, main *mygo.Window, off bool) {
	ctl := newNookCtl(notch.System{}, nil, nil, app)
	newWin := func(view func(*ui.Context), mouse bool) *mygo.Window {
		w := mygo.NewWindow(mygo.WindowOptions{Title: "Hotwatch Nook", Width: 10, Height: 10,
			Frameless: true, Transparent: true, AlwaysOnTop: true, SkipTaskbar: true, Hidden: true,
			DisableResize: true, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true, DisableShadow: true,
			Content: ui.View(view)})
		w.SetVisibleOnAllWorkspaces(true)
		w.SetIgnoreMouseEvents(!mouse)
		overMenuBar(w.NativeHandle())
		return w
	}
	bar := newWin(func(cx *ui.Context) {
		cx.Root().Background(ui.Transparent)
		nook.Bar(cx, app.HostView(app.Selected()))
	}, false)
	var island *mygo.Window
	bringMain := func() {
		main.Show()
		main.Focus()
	}
	island = newWin(func(cx *ui.Context) {
		cx.Root().Background(ui.Transparent)
		now := time.Now()
		ctl.motion.Update(now, ctl.hover)
		nook.Island(cx, ctl.islandInput(now,
			func(id string) { app.SelectHost(id); main.Invalidate(); bar.Invalidate() },
			func(page string) { app.ShowPage(page); main.Invalidate(); bringMain() },
			bringMain,
			func() { app.TogglePause(); main.Invalidate() }))
		if ctl.motion.Animating() {
			cx.After(nook.FrameDelay())
		}
	}, true)
	ctl.bar, ctl.island = mygoWin{bar}, mygoWin{island}
	ctl.off = off
	ctl.relayout()
	c.nook = ctl

	go func() {
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		last := time.Now()
		for {
			select {
			case <-c.done:
				return
			case now := <-t.C:
				pt := mygo.Screen.CursorScreenPoint()
				relayout := now.Sub(last) >= relayoutEvery
				if relayout {
					last = now
				}
				mygo.RunOnMain(func() {
					if relayout {
						ctl.relayout()
					}
					ctl.tick(now, float64(pt.X), float64(pt.Y))
				})
			}
		}
	}()
}
