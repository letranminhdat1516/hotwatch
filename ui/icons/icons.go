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

	ChevronDown  = load("chevron-down")
	ChevronRight = load("chevron-right")
	Pause        = load("pause")
	Play         = load("play")
	Battery      = load("battery")
	Thermometer  = load("thermometer")
	Info         = load("info")
	Laptop       = load("laptop")
	Activity     = load("activity")
)

// symbolNames are the SF Symbols the Mac draws in place of each icon (see
// hotwatch/mac/symbols); an icon without one is always drawn from Lucide.
var symbolNames = map[*ui.SVG]string{
	Overview: "square.grid.2x2.fill", CPU: "cpu.fill", Memory: "memorychip.fill", Disk: "internaldrive.fill",
	Network: "network", GPU: "cube.fill", Energy: "bolt.fill", Processes: "list.bullet", Docker: "shippingbox.fill",
	Apps: "square.grid.3x3.fill", Packages: "shippingbox", Settings: "gearshape.fill", Host: "server.rack",
	Offline: "wifi.slash", Warning: "exclamationmark.triangle.fill", ChevronDown: "chevron.down",
	ChevronRight: "chevron.right", Pause: "pause.fill", Play: "play.fill", Battery: "battery.75percent",
	Thermometer: "thermometer.medium", Info: "info.circle.fill", Laptop: "laptopcomputer", Activity: "waveform.path.ecg",
}

// Symbol is the SF Symbol name for icon, "" when it has none.
func Symbol(icon *ui.SVG) string { return symbolNames[icon] }
