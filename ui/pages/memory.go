package pages

import (
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// segments splits memory into the parts the platform reports: App, Wired and
// Compressed where those are set (macOS), otherwise Used, Cached and
// Buffers (Linux); Info.OS decides only if both are set. Free is what the
// parts leave of Total.
func segments(m model.Memory, os string) []components.Segment {
	mac := m.App+m.Wired+m.Compressed > 0
	linux := m.Cached+m.Buffers > 0
	if mac && linux {
		mac = os == "darwin"
	}
	var segs []components.Segment
	switch {
	case mac:
		segs = []components.Segment{{Label: "App", Value: m.App}, {Label: "Wired", Value: m.Wired}, {Label: "Compressed", Value: m.Compressed}}
	case linux:
		segs = []components.Segment{{Label: "Used", Value: m.Used}, {Label: "Cached", Value: m.Cached}, {Label: "Buffers", Value: m.Buffers}}
	default:
		segs = []components.Segment{{Label: "Used", Value: m.Used}}
	}
	var sum uint64
	for _, s := range segs {
		sum += s.Value
	}
	free := uint64(0)
	if m.Total > sum {
		free = m.Total - sum
	}
	return append(segs, components.Segment{Label: "Free", Value: free, Muted: true})
}

// Memory builds the Memory page: the memory split, swap, pressure, the last
// five minutes of use and the largest processes.
func Memory(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	known := !partial(s, "memory") && s.Memory.Total > 0
	pageColumn(c, func() {
		section(c, "Memory", func() {
			if !known {
				note(c, format.Unknown)
				return
			}
			m := s.Memory
			ui.Row(c).Gap(2 * g()).AlignItems(ui.Center).Wrap().Children(func() {
				ui.RichText(c,
					ui.Span{Text: format.Bytes(m.Used), Size: 28, Weight: 600},
					ui.Span{Text: " of " + format.Bytes(m.Total), Size: 15, Weight: 500, Color: c.Theme().TextMuted}).SingleLine()
				if m.Pressure != "" {
					components.Badge(c, m.Pressure)
				}
			})
			components.StackedBar(c, segments(m, v.Info.OS))
			swap := format.Unknown
			if m.SwapTotal > 0 {
				swap = format.Bytes(m.SwapUsed) + " / " + format.Bytes(m.SwapTotal)
			}
			note(c, "Swap "+swap)
		})
		components.TimeChart(c, "Memory", "%", map[string][]hub.Point{"Used": points(v, "mem")}, now)
		procSection(c, v, "Top processes by memory", func() {
			// RSS 0 means unreadable (macOS), not an empty process.
			top := topProcs(v.Procs, func(p model.Process) bool { return p.RSS > 0 }, func(p model.Process) float64 { return float64(p.RSS) })
			table(c, []column{{title: "Name"}, {title: "PID", width: 8 * g(), end: true}, {title: "Memory", width: 10 * g(), end: true}},
				len(top), func(r, col int) {
					switch col {
					case 0:
						text(c, top[r].Name, false)
					case 1:
						text(c, pid(top[r]), true)
					case 2:
						text(c, format.Bytes(top[r].RSS), true)
					}
				})
		})
	})
}
