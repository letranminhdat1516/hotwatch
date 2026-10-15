package pages

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// aboutShowSerial is whether the serial number is printed in full; it is
// hidden until the user asks, as it identifies the machine in screenshots.
// Main thread only.
var aboutShowSerial bool

// About builds the "About This Mac" page: a hero with the model, chip and
// memory, then Hardware, Software, Displays and Storage cards. A fact the
// host does not report is left out, never shown as "—".
func About(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	t := c.Theme()
	s, m := v.Snap, v.Info.Machine
	pageColumn(c, func() {
		components.Surface(c, ui.Row(c).Gap(6*g()).AlignItems(ui.Center).Padding(5*g(), 6*g())).Children(func() {
			macDrawing(c, 150)
			ui.Column(c).Gap(g() / 2).Children(func() {
				ui.Text(c, orDash(m.Model)).FontSize(34).FontWeight(700).SingleLine()
				ui.Text(c, chipLine(m, s)).FontSize(17).TextColor(t.TextMuted).SingleLine()
				ui.Row(c).Gap(g()).Margin(g(), 0, 0, 0).Wrap().Children(func() {
					if m.OSVersion != "" {
						osLine := "macOS " + m.OSVersion
						if m.Build != "" {
							osLine += " (" + m.Build + ")"
						}
						dotPill(c, osLine, ui.Hex("#0a84ff"))
					}
					if s.Uptime > 0 {
						dotPill(c, "Up "+format.Duration(s.Uptime), t.Success)
					}
				})
			})
		})
		ui.Grid(c).Columns(2).Gap(2 * g()).Children(func() {
			aboutCard(c, icons.CPU, "Hardware", "cpu", func() {
				fact(c, "Model", m.Model)
				fact(c, "Model identifier", m.ModelID)
				fact(c, "Chip", m.Chip)
				fact(c, "CPU cores", coresText(v.Info.CPUCount, m))
				if m.GPUCores > 0 {
					fact(c, "GPU cores", fmt.Sprint(m.GPUCores))
				}
				if s.Memory.Total > 0 {
					fact(c, "Memory", format.Bytes(s.Memory.Total))
				}
				fact(c, "Architecture", archName(v.Info.OS, v.Info.Arch))
				if m.Serial != "" {
					ui.Row(c).Gap(g()).AlignItems(ui.Center).Children(func() {
						serial := strings.Repeat("•", max(len(m.Serial)-2, 0)) + m.Serial[max(len(m.Serial)-2, 0):]
						label := "Show"
						if aboutShowSerial {
							serial, label = m.Serial, "Hide"
						}
						ui.Text(c, "Serial number").FontSize(14).TextColor(t.TextMuted).SingleLine()
						ui.Spacer(c)
						ui.Text(c, serial).FontSize(14).FontWeight(600).SingleLine()
						if ui.Button(c, label).Clicked() {
							aboutShowSerial = !aboutShowSerial
						}
					})
				}
			})
			aboutCard(c, icons.Laptop, "Software", "", func() {
				if m.OSVersion != "" {
					fact(c, "System", "macOS "+m.OSVersion)
				}
				fact(c, "Build", m.Build)
				fact(c, "Kernel", v.Info.Kernel)
				fact(c, "Computer name", v.Info.Hostname)
				if s.Uptime > 0 {
					fact(c, "Started", now.Add(-s.Uptime).Local().Format("Mon, Jan 2 2006 at 15:04"))
					fact(c, "Uptime", format.Duration(s.Uptime))
				}
				if n := len(v.Procs); n > 0 {
					fact(c, "Processes", fmt.Sprint(n))
				}
				fact(c, "Hotwatch", appVersion()+" · "+runtime.Version())
			})
			if m.DisplayW > 0 && m.DisplayH > 0 {
				aboutCard(c, icons.Monitor, "Displays", "network", func() {
					size := fmt.Sprintf("%d × %d", m.DisplayW, m.DisplayH)
					if m.DisplayScale > 1 {
						size += fmt.Sprintf(" @%dx", m.DisplayScale)
					}
					fact(c, "Main display", size)
				})
			}
			if vols := model.DistinctVolumes(s.Disks); len(vols) > 0 {
				aboutCard(c, icons.Disk, "Storage", "disk", func() {
					for _, d := range vols {
						if d.Image {
							continue
						}
						name := d.Name
						if name == "" {
							name = d.Mount
						}
						free := d.Total - min(d.Used, d.Total)
						fact(c, name, format.Bytes(free)+" free of "+format.Bytes(d.Total))
					}
				})
			}
		})
	})
}

// aboutCard is a card with a tinted icon tile and title, in the metric
// colour of key when it has one.
func aboutCard(c *ui.Context, icon *ui.SVG, title, key string, body func()) {
	components.Surface(c, ui.Column(c).Gap(g()+2)).Children(func() {
		overviewSectionHeader(c, icon, title, key)
		body()
	})
}

// fact is a kv row, omitted when the value is unknown.
func fact(c *ui.Context, label, value string) {
	if value == "" {
		return
	}
	kvRow(c, label, value)
}

// archName is "Apple silicon" for arm64 Macs and "Intel" for x86 ones; the
// architecture as reported elsewhere.
func archName(os, arch string) string {
	if os == "darwin" {
		switch arch {
		case "arm64":
			return "Apple silicon"
		case "amd64":
			return "Intel"
		}
	}
	return arch
}

// dotPill is a tinted pill with a dot: "● macOS 26.3.1 (25D2128)".
func dotPill(c *ui.Context, text string, col ui.Color) {
	ui.Row(c).Gap(6).Padding(4, 10).Radius(999).AlignItems(ui.Center).Background(col.Alpha(0.16)).Children(func() {
		ui.Box(c).Size(7, 7).Radius(3.5).Background(col)
		ui.Text(c, text).FontSize(14).FontWeight(650).TextColor(col).SingleLine()
	})
}

// macDrawing is a laptop drawn in shapes, width w DIPs wide: the bezel, a
// blue screen lit from the top, the camera notch and the silver base.
func macDrawing(c *ui.Context, w float32) ui.Element {
	h := w * 0.62
	return ui.Box(c).Size(w, h).Shrink(0).Label("Mac").Draw(func(p *ui.Painter, r ui.Rect) {
		base := r.H * 0.07
		lidW := r.W * 0.84
		lid := ui.Rect{X: r.X + (r.W-lidW)/2, Y: r.Y, W: lidW, H: r.H - base}
		p.Fill(lid, ui.Hex("#2c2c2e"), 7)
		p.Fill(ui.Rect{X: lid.X + 1, Y: lid.Y + 1, W: lid.W - 2, H: lid.H - 1}, ui.Hex("#0d0d0f"), 6)
		scr := ui.Rect{X: lid.X + 5, Y: lid.Y + 5, W: lid.W - 10, H: lid.H - 8}
		p.Fill(scr, ui.Hex("#2f6fd0"), 2)
		bands := 12
		for i := 0; i < bands; i++ { // a soft gradient, lighter towards the top
			y := scr.Y + scr.H*float32(i)/float32(bands)
			p.Fill(ui.Rect{X: scr.X, Y: y, W: scr.W, H: scr.H / float32(bands)}, ui.Hex("#9cc8ff").Alpha(0.5*float32(bands-i)/float32(bands)), 0)
		}
		notch := lid.W * 0.12
		p.Fill(ui.Rect{X: lid.X + (lid.W-notch)/2, Y: lid.Y + 1, W: notch, H: 5}, ui.Hex("#0d0d0f"), 2)
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - base, W: r.W, H: base}, ui.Hex("#d1d1d6"), base/2)
		p.Fill(ui.Rect{X: r.X + r.W*0.42, Y: r.Y + r.H - base, W: r.W * 0.16, H: base * 0.45}, ui.Hex("#a1a1a6"), base/4)
	})
}

// pillText is a neutral pill.
func pillText(c *ui.Context, text string) {
	t := c.Theme()
	ui.Row(c).Padding(3, 10).Radius(999).Background(t.SurfaceHover).Children(func() {
		ui.Text(c, text).FontSize(12).FontWeight(600).SingleLine()
	})
}

// chipLine is "Apple M5 · 24.0 GB", whichever parts are known.
func chipLine(m model.Machine, s *model.Snapshot) string {
	var parts []string
	if m.Chip != "" {
		parts = append(parts, m.Chip)
	}
	if s.Memory.Total > 0 {
		parts = append(parts, format.Bytes(s.Memory.Total))
	}
	return strings.Join(parts, " · ")
}

// coresText is "10 (4 performance, 6 efficiency)" when the split is known,
// "10" when only the count is, "" when neither.
func coresText(count int, m model.Machine) string {
	if m.PerfCores > 0 && m.EffCores > 0 {
		return fmt.Sprintf("%d (%d Performance, %d Efficiency)", m.PerfCores+m.EffCores, m.PerfCores, m.EffCores)
	}
	if count > 0 {
		return fmt.Sprint(count)
	}
	return ""
}
