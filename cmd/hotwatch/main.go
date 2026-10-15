// Command hotwatch is the Mac app: this Mac and the agent hosts in one window.
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/store"
	hwui "hotwatch/ui"
)

func main() {
	start := time.Now()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)
	fatal := func(msg string, err error) {
		log.Error(msg, "err", err)
		os.Exit(1)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fatal("home directory", err)
	}
	p := appPaths(home)
	cfg, warn, err := store.Load(p.Config)
	if err != nil {
		fatal("load config", err)
	}
	secrets, err := newSecrets()
	if err != nil {
		fatal("keychain", err)
	}

	mygo.App.SetName("Hotwatch")
	var win *mygo.Window // set on the main thread before anything posts
	c := newCore(cfg, p, secrets, newLocalSource, func(f func()) { win.Update(f) }, log)
	c.times.start = start
	mygo.App.WhenReady(func() {
		app := hwui.NewApp(func(string) hub.HostView { return hub.HostView{} }, cfg.Hosts, c.runner, c.onSelect)
		c.attach(app, warn)
		// The sidebar draws under the window controls (ui.Context.TitleBar).
		win = mygo.NewWindow(mygo.WindowOptions{Title: "Hotwatch", Width: 1280, Height: 800, MinWidth: 900, MinHeight: 560,
			TitleBarStyle: mygo.TitleBarHiddenInset, TrafficLightPosition: &mygo.Point{X: 14, Y: 14},
			Content: ui.View(app.View)})
		c.startHub()
	})
	err = mygo.App.Run()
	c.shutdown()
	if err != nil {
		fatal("app", err)
	}
}
