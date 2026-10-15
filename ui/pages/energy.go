package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
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

// Energy builds the Energy page: the charge and its state, battery power
// and temperature, the thermal state and what keeps the Mac awake.
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
		section(c, "Battery", func() {
			if b == nil {
				ui.Text(c, format.Unknown).FontSize(28).FontWeight(600).SingleLine()
				kv(c, "Time remaining", format.Unknown)
				kv(c, "Battery power", format.Unknown)
				kv(c, "Battery temperature", format.Unknown)
				kv(c, "Thermal state", format.Unknown)
				return
			}
			ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, format.Percent(&b.ChargePct)).FontSize(28).FontWeight(600).SingleLine()
				components.Badge(c, powerState(b))
			})
			remaining := format.Unknown
			if b.TimeRemaining != nil {
				remaining = format.Duration(*b.TimeRemaining)
			}
			kv(c, "Time remaining", remaining)
			power := format.Unknown
			if n := format.Number(b.PowerW); n != format.Unknown {
				power = n + " W"
			}
			kv(c, "Battery power", power)
			kv(c, "Battery temperature", celsius(b.TempC))
			ui.Row(c).Gap(g()).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Thermal state").FontSize(12).TextColor(c.Theme().TextMuted).Width(18 * g()).SingleLine()
				if b.ThermalState == "" {
					ui.Text(c, format.Unknown).FontSize(13).FontWeight(600).SingleLine()
				} else {
					components.Badge(c, b.ThermalState)
				}
			})
		})
		section(c, "Keeping the Mac awake", func() {
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
	})
}
