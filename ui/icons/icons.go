// Package icons holds the Lucide icons hotwatch shows (ISC license, see
// LICENSE), from lucide-static 0.460.0.
package icons

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

//go:embed svg/*.svg
var files embed.FS

func load(name string) *ui.SVG {
	b, err := files.ReadFile("svg/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(b)
}

var (
	Overview  = load("layout-dashboard")
	CPU       = load("cpu")
	Memory    = load("memory-stick")
	Disk      = load("hard-drive")
	Network   = load("network")
	GPU       = load("microchip")
	Energy    = load("zap")
	Processes = load("list")
	Docker    = load("container")
	Apps      = load("app-window")
	Packages  = load("package")
	Settings  = load("settings")
	Host      = load("server")
	Offline   = load("wifi-off")
	Warning   = load("triangle-alert")

	ChevronDown = load("chevron-down")
	ChevronRight = load("chevron-right")
	Pause        = load("pause")
	Play         = load("play")
	Battery      = load("battery")
	Thermometer  = load("thermometer")
)
