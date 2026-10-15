package pages

import (
	"fmt"
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

// Energy builds the Energy page (UI-DESIGN.md, "Energy"): the battery ring
// with its figures, the power card with its history, then the temperature
// and sleep-assertion cards. Figures the host does not report are dashes;
// cards with nothing to show are left out, never faked.
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
		if b != nil && b.PowerW != nil {
			powerCard(c, b, v, now)
		}
		ui.Grid(c).Columns(2).Gap(2 * g()).Children(func() {
			if b != nil && b.TempC != nil {
				temperaturesCard(c, b)
			}
			awakeCard(c, b)
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

// batteryCard is the charge ring beside six figures about the battery.
func batteryCard(c *ui.Context, b *model.Battery) {
	tint, _ := components.MetricColor("battery")
	components.Surface(c, ui.Column(c).Gap(2*g())).Children(func() {
		overviewSectionHeader(c, icons.Battery, "Battery", "battery")
		ui.Row(c).Gap(4 * g()).AlignItems(ui.Center).Children(func() {
			if b == nil {
				components.Ring(c, 0, 14*g(), tint, format.Unknown, "")
			} else {
				components.Ring(c, b.ChargePct/100, 14*g(), tint, format.Percent(&b.ChargePct), powerLabel(b))
			}
			remaining, flow, flowSub := format.Unknown, format.Unknown, ""
			adapter, temp, awake := format.Unknown, format.Unknown, format.Unknown
			if b != nil {
				if b.TimeRemaining != nil {
					remaining = format.Duration(*b.TimeRemaining)
				}
				if n := format.Number(b.PowerW); n != format.Unknown {
					flow, flowSub = n+" W", "discharging"
					switch {
					case b.Charging:
						flowSub = "charging"
					case b.OnAC:
						flowSub = "from the adapter"
					}
				}
				adapter = "None"
				if b.OnAC {
					adapter = "Connected"
				}
				temp = celsius(b.TempC)
				switch n := len(b.Assertions); n {
				case 0:
					awake = "Nothing"
				case 1:
					awake = "1 assertion"
				default:
					awake = fmt.Sprintf("%d assertions", n)
				}
			}
			ui.Grid(c).Columns(3).GapX(3 * g()).GapY(2 * g()).Grow(1).Children(func() {
				statCell(c, "Remaining", remaining, "")
				statCell(c, "Battery Flow", flow, flowSub)
				statCell(c, "Adapter", adapter, "")
				statCell(c, "Temperature", temp, "")
				thermalCell(c, b)
				statCell(c, "Awake", awake, "")
			})
		})
	})
}

// powerCard is the battery's power draw with its window average and peak,
// the thermal state, and the power history as a dot chart.
func powerCard(c *ui.Context, b *model.Battery, v hub.HostView, now time.Time) {
	t := c.Theme()
	tint, _ := components.MetricColor("energy")
	from := now.Add(-components.ChartWindow())
	components.Surface(c, ui.Column(c).Gap(g())).Children(func() {
		overviewSectionHeader(c, icons.Energy, "Power", "energy")
		sub := "The whole computer draws"
		switch {
		case b.Charging:
			sub = "The battery charges at"
		case b.OnAC:
			sub = "Battery power"
		}
		ui.Text(c, sub).FontSize(13).TextColor(t.TextMuted).Margin(g(), 0, 0, 0)
		ui.Row(c).Gap(4 * g()).AlignItems(ui.Center).Children(func() {
			ui.RichText(c, ui.Span{Text: format.Number(b.PowerW), Size: 40, Weight: 700},
				ui.Span{Text: " W", Size: 16, Weight: 500, Color: t.TextMuted}).SingleLine()
			avg, peak := format.Unknown, format.Unknown
			if v.Hist != nil {
				if a, p, ok := v.Hist.Stats("power", from); ok {
					avg, peak = format.Number(&a)+" W", format.Number(&p)+" W"
				}
			}
			statCell(c, "Average", avg, "")
			statCell(c, "Peak", peak, "")
			thermalCell(c, b)
		})
		components.DotChart(c, points(v, "power"), from, now, 0, tint, 160).Margin(g(), 0, 0, 0)
	})
}

// temperaturesCard lists the temperatures the host reports (the battery's).
func temperaturesCard(c *ui.Context, b *model.Battery) {
	components.Surface(c, ui.Column(c).Gap(g()+4)).Children(func() {
		overviewSectionHeader(c, icons.Thermometer, "Temperatures", "energy")
		kv(c, "Battery", celsius(b.TempC))
	})
}

// awakeCard lists what keeps the Mac awake.
func awakeCard(c *ui.Context, b *model.Battery) {
	components.Surface(c, ui.Column(c).Gap(g()+4)).Children(func() {
		overviewSectionHeader(c, icons.Processes, "Keeping the Mac awake", "")
		switch {
		case b == nil:
			note(c, format.Unknown)
		case len(b.Assertions) == 0:
			components.EmptyState(c, "Nothing is preventing sleep")
		default:
			as := b.Assertions
			table(c, []column{{title: "Process"}, {title: "PID", width: 8 * g(), end: true}, {title: "Type"}, {title: "Reason"}},
				len(as), func(r, col int) {
					switch col {
					case 0:
						text(c, as[r].Process, false)
					case 1:
						text(c, fmt.Sprint(as[r].PID), true)
					case 2:
						text(c, as[r].Type, false)
					case 3:
						text(c, as[r].Reason, false)
					}
				})
		}
	})
}
