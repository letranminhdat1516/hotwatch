package pages

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// powerState is the badge key of where the Mac's power comes from.
func powerState(b *model.Battery) string {
	switch {
	case b.Charging:
		return "charging"
	case b.OnAC:
		return "on ac"
	}
	return "on battery"
}

// powerLabel is powerState as shown.
func powerLabel(b *model.Battery) string {
	switch powerState(b) {
	case "charging":
		return "Charging"
	case "on ac":
		return "On AC"
	}
	return "On battery"
}

// Energy builds the Energy page (the owner's reference): the battery ring
// with its figures, what the whole computer draws over a dot chart, the
// apps using the most energy, the temperatures, what keeps the Mac awake
// and where the power comes from. Figures the host does not report are
// left out or dashed, never faked.
func Energy(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	unknown := partial(s, "battery")
	if s.Battery == nil && !unknown {
		components.EmptyState(c, "No battery on this host")
		return
	}
	b := s.Battery // nil: values unknown
	if unknown {
		b = nil
	}
	pageColumn(c, func() {
		batteryCard(c, b)
		if b != nil && (b.SystemW != nil || b.PowerW != nil) {
			powerCard(c, b, v, now)
		}
		apps := energyByApp(v.Procs)
		ui.Grid(c).Columns(2).Gap(2 * g()).Children(func() {
			if len(apps) > 0 {
				energyCard(c, apps)
			}
			temperaturesCard(c, s, b)
			if v.Info.OS == "darwin" { // sleep assertions are read on macOS only
				awakeCard(c, b)
			}
			if b != nil {
				powerSourceCard(c, b)
			}
		})
	})
}

// statCell is a label over a value, with an optional muted sub-line.
func statCell(c *ui.Context, label, value, sub string) {
	t := c.Theme()
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).SingleLine()
		ui.Text(c, value).FontSize(15).FontWeight(600).SingleLine()
		if sub != "" {
			ui.Text(c, sub).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}
	})
}

// thermalCell is the Thermal stat: the state badge, or a dash.
func thermalCell(c *ui.Context, b *model.Battery) {
	t := c.Theme()
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, "Thermal").FontSize(12).TextColor(t.TextMuted).SingleLine()
		if b == nil || b.ThermalState == "" {
			ui.Text(c, format.Unknown).FontSize(15).FontWeight(600).SingleLine()
			return
		}
		ui.Row(c).Children(func() { components.Badge(c, b.ThermalState) })
	})
}

// batteryCard is the charge ring beside six figures about the battery,
// with the thermal state as a badge.
func batteryCard(c *ui.Context, b *model.Battery) {
	tint, _ := components.MetricColor("battery")
	right := func() {
		if b != nil && b.ThermalState != "" {
			label, tone := thermalLabel(b.ThermalState)
			statusPill(c, label, tone)
		}
	}
	pageCard(c, icons.Battery, "Battery", "battery", right, func() {
		ui.Row(c).Gap(5*g()).AlignItems(ui.Center).Margin(g(), 0, 0, 0).Children(func() {
			if b == nil {
				components.Ring(c, 0, 17*g(), tint, format.Unknown, "")
			} else {
				components.Ring(c, b.ChargePct/100, 17*g(), tint, fmt.Sprintf("%.0f%%", b.ChargePct), powerLabel(b))
			}
			figs := []fig{{"Remaining", format.Unknown, ""}, {"Battery Flow", format.Unknown, ""}, {"Health", format.Unknown, ""},
				{"Cycles", format.Unknown, ""}, {"Capacity", format.Unknown, ""}, {"Adapter", "None", ""}}
			if b != nil {
				if b.Charging {
					figs[0].label = "Until Full"
				}
				if b.TimeRemaining != nil {
					figs[0].value = format.Duration(*b.TimeRemaining)
				} else if b.OnAC && !b.Charging {
					figs[0].value = "On AC"
				}
				if n := format.Number(b.PowerW); n != format.Unknown {
					figs[1].value, figs[1].sub = n+" W", "discharging"
					switch {
					case b.Charging:
						figs[1].sub = "charging"
					case b.OnAC:
						figs[1].sub = "on adapter"
					}
				}
				if b.HealthPct != nil {
					figs[2].value, figs[2].sub = fmt.Sprintf("%.0f%%", *b.HealthPct), "of design capacity"
				}
				if b.Cycles != nil {
					figs[3].value = thousands(*b.Cycles)
				}
				if b.CapacityMAh != nil {
					figs[4].value = thousands(*b.CapacityMAh) + " mAh"
					if b.DesignMAh != nil {
						figs[4].sub = "design " + thousands(*b.DesignMAh)
					}
				}
				switch {
				case b.AdapterW != nil:
					figs[5].value, figs[5].sub = fmt.Sprintf("%.0f W", *b.AdapterW), b.AdapterName
				case b.OnAC:
					figs[5].value = "Connected"
				}
			}
			ui.Column(c).Grow(1).Children(func() { statGrid(c, 3, figs) })
		})
	})
}

// powerCard is what the whole computer draws (the battery's flow where the
// Mac does not say), what the processes account for, the window's average
// and peak and the thermal state, over the power history.
func powerCard(c *ui.Context, b *model.Battery, v hub.HostView, now time.Time) {
	tint, _ := components.MetricColor("energy")
	from := now.Add(-components.ChartWindow())
	series, label, w := "power.system", "The whole computer draws", b.SystemW
	if w == nil {
		series, label, w = "power", "The battery's flow", b.PowerW
	}
	pageCard(c, icons.Energy, "Power", "energy", nil, func() {
		var figs []fig
		if procs, ok := sumPtr(v.Procs, func(p model.Process) *float64 { return p.PowerMW }); ok {
			figs = append(figs, fig{"Processes", watts(procs), ""})
		}
		avg, peak := format.Unknown, format.Unknown
		if v.Hist != nil {
			if a, p, ok := v.Hist.Stats(series, from); ok {
				avg, peak = format.Number(&a)+" W", format.Number(&p)+" W"
			}
		}
		figs = append(figs, fig{"Average", avg, ""}, fig{"Peak", peak, ""})
		if b.ThermalState != "" {
			th, _ := thermalLabel(b.ThermalState)
			figs = append(figs, fig{"Thermal", th, thermalFeel(b.ThermalState)})
		}
		headline(c, label, format.Number(w), "W", figs)
		components.DotChart(c, points(v, series), from, now, 0, tint, 200).Margin(g(), 0, 0, 0)
	})
}

// temperaturesCard is a bar per temperature the host reports: the
// processor's (with its hottest die), the storage's and the battery's,
// on a 0–100 °C scale that warms from green through orange to red.
func temperaturesCard(c *ui.Context, s *model.Snapshot, b *model.Battery) {
	type temp struct {
		label, sub string
		c          float64
	}
	var ts []temp
	if s.CPU.TempC != nil {
		sub := ""
		if s.CPU.TempMaxC != nil {
			sub = "hottest " + degrees(*s.CPU.TempMaxC)
		}
		ts = append(ts, temp{"Processor", sub, *s.CPU.TempC})
	}
	if s.StorageTempC != nil {
		ts = append(ts, temp{"Storage", "", *s.StorageTempC})
	}
	if b != nil && b.TempC != nil {
		ts = append(ts, temp{"Battery", "", *b.TempC})
	}
	if len(ts) == 0 {
		return
	}
	pageCard(c, icons.Thermometer, "Temperatures", "energy", nil, func() {
		for _, x := range ts {
			labelBar(c, x.label, x.sub, x.c/100, tempColor(c, x.c), degrees(x.c))
		}
	})
}

// tempColor is green while cool, orange from 70 °C, red from 90 °C.
func tempColor(c *ui.Context, deg float64) ui.Color {
	switch {
	case deg >= 90:
		return c.Theme().Danger
	case deg >= 70:
		return ui.Hex("#ff9f0a")
	}
	return ui.Hex("#30d158")
}

// awakeCard lists what keeps the Mac awake: each app with its reason and
// whether it holds the system or the display awake.
func awakeCard(c *ui.Context, b *model.Battery) {
	t := c.Theme()
	pageCard(c, icons.Coffee, "Keeping Awake", "energy", nil, func() {
		switch {
		case b == nil:
			note(c, format.Unknown)
		case len(b.Assertions) == 0:
			note(c, "Nothing is keeping the Mac awake.")
		default:
			for i, a := range b.Assertions[:min(len(b.Assertions), 6)] {
				ui.Row(c).Key(fmt.Sprintf("awake-%d", i)).Gap(g() + 4).AlignItems(ui.Center).Children(func() {
					appIcon(c, a.Process, "")
					ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
						ui.Text(c, a.Process).FontSize(14).SingleLine()
						if a.Reason != "" {
							ui.Text(c, a.Reason).FontSize(12).TextColor(t.TextMuted).SingleLine()
						}
					})
					ui.Row(c).Padding(4, 10).Radius(12).Background(t.Warning.Alpha(0.14)).Children(func() {
						ui.Text(c, "● "+assertionKind(a.Type)).FontSize(13).FontWeight(650).TextColor(t.Warning).SingleLine()
					})
				})
			}
			if n := len(b.Assertions) - 6; n > 0 {
				note(c, fmt.Sprintf("And %d more.", n))
			}
		}
	})
}

// powerSourceCard says where the power comes from.
func powerSourceCard(c *ui.Context, b *model.Battery) {
	pageCard(c, icons.Plug, "Power Source", "energy", nil, func() {
		from := "Battery"
		if b.OnAC {
			from = "Power Adapter"
		}
		kvRow(c, "Drawing from", from)
		if b.AdapterName != "" {
			kvRow(c, "Adapter", b.AdapterName)
		}
		if b.AdapterW != nil {
			kvRow(c, "Adapter rating", fmt.Sprintf("%.0f W", *b.AdapterW))
		}
		kvRow(c, "Battery", fmt.Sprintf("%.0f%% · %s", b.ChargePct, powerLabel(b)))
		if b.ThermalState != "" {
			th, _ := thermalLabel(b.ThermalState)
			kvRow(c, "Thermal state", th+" · "+thermalFeel(b.ThermalState))
		}
		if b.SystemW != nil {
			kvRow(c, "Whole computer", format.Number(b.SystemW)+" W")
		}
	})
}

// thermalLabel is the thermal state as shown, with its tone.
func thermalLabel(state string) (string, string) {
	switch state {
	case "nominal":
		return "Normal", "ok"
	case "fair":
		return "Fair", "warn"
	case "serious":
		return "Serious", "bad"
	case "critical":
		return "Critical", "bad"
	}
	return state, "ok"
}

// thermalFeel says how the state feels.
func thermalFeel(state string) string {
	switch state {
	case "nominal":
		return "Cool"
	case "fair":
		return "Warm"
	case "serious":
		return "Hot"
	case "critical":
		return "Throttling"
	}
	return ""
}

// assertionKind names what an assertion holds awake.
func assertionKind(typ string) string {
	if strings.Contains(typ, "Display") {
		return "Display"
	}
	return "System"
}
