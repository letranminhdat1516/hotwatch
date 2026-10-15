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
		components.Surface(c, ui.Row(c).Gap(3*g()).AlignItems(ui.Center)).Children(func() {
			ui.Row(c).Size(9*g(), 9*g()).Radius(2 * g()).Center().Background(t.Accent.Alpha(0.14)).Children(func() {
				ui.Icon(c, icons.Laptop).FontSize(40).TextColor(t.Accent)
			})
			ui.Column(c).Gap(g() / 2).Children(func() {
				ui.Text(c, orDash(m.Model)).FontSize(32).FontWeight(700).SingleLine()
				ui.Text(c, chipLine(m, s)).FontSize(15).TextColor(t.TextMuted).SingleLine()
				ui.Row(c).Gap(g()).Margin(g()/2, 0, 0, 0).Wrap().Children(func() {
					if m.OSVersion != "" {
						pillText(c, "macOS "+m.OSVersion)
					}
					if s.Uptime > 0 {
						pillText(c, "Up "+format.Duration(s.Uptime))
					}
					if s.Battery != nil && s.Battery.ThermalState != "" {
						components.Badge(c, s.Battery.ThermalState)
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
				fact(c, "Architecture", v.Info.Arch)
				if m.Serial != "" {
					ui.Row(c).Gap(g()).AlignItems(ui.Center).Children(func() {
						serial := strings.Repeat("•", max(len(m.Serial)-2, 0)) + m.Serial[max(len(m.Serial)-2, 0):]
						label := "Show"
						if aboutShowSerial {
							serial, label = m.Serial, "Hide"
						}
						kv(c, "Serial", serial)
						if ui.Button(c, label).Clicked() {
							aboutShowSerial = !aboutShowSerial
						}
					})
				}
			})
			aboutCard(c, icons.Info, "Software", "", func() {
				if m.OSVersion != "" {
					fact(c, "System", "macOS "+m.OSVersion)
				}
				fact(c, "Build", m.Build)
				fact(c, "Kernel", v.Info.Kernel)
				fact(c, "Computer name", v.Info.Hostname)
				if s.Uptime > 0 {
					fact(c, "Started", now.Add(-s.Uptime).Local().Format("Jan 2, 15:04"))
					fact(c, "Uptime", format.Duration(s.Uptime))
				}
				if n := len(v.Procs); n > 0 {
					fact(c, "Processes", fmt.Sprint(n))
				}
				fact(c, "Hotwatch", appVersion()+" · "+runtime.Version())
			})
			if m.DisplayW > 0 && m.DisplayH > 0 {
				aboutCard(c, icons.Laptop, "Displays", "", func() {
					size := fmt.Sprintf("%d × %d", m.DisplayW, m.DisplayH)
					if m.DisplayScale > 1 {
						size += fmt.Sprintf(" @%dx", m.DisplayScale)
					}
					fact(c, "Main display", size)
				})
			}
			if vols := model.DistinctVolumes(s.Disks); len(vols) > 0 {
				aboutCard(c, icons.Disk, "Storage", "disk", func() {
					var used, total uint64
					for _, d := range vols {
						used, total = used+d.Used, total+d.Total
					}
					if total > 0 {
						free := total - min(used, total)
						ui.Text(c, format.Bytes(free)+" free of "+format.Bytes(total)).FontSize(13).FontWeight(600).SingleLine()
						components.BarRow(c, "Used", ratio(used, total), format.Percent(ratio(used, total)), metricTint(c, "disk"))
					}
					for _, d := range vols {
						desc := format.Bytes(d.Used) + " used"
						if d.FS != "" {
							desc = d.FS + " · " + desc
						}
						fact(c, d.Mount, desc)
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
	kv(c, label, value)
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
		return fmt.Sprintf("%d (%d performance, %d efficiency)", m.PerfCores+m.EffCores, m.PerfCores, m.EffCores)
	}
	if count > 0 {
		return fmt.Sprint(count)
	}
	return ""
}
