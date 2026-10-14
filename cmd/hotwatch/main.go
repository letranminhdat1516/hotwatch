package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Hotwatch", Content: ui.View(func(c *ui.Context) {
			ui.Text(c, "Hotwatch")
		})})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
