package pages

import (
	"fmt"
	"path"
	"slices"
	"strings"
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
			swap := "off"
			if m.SwapTotal > 0 {
				swap = format.Bytes(m.SwapUsed) + " / " + format.Bytes(m.SwapTotal)
			}
			note(c, "Swap "+swap)
		})
		components.DotTimeChart(c, "Memory usage", "%", []components.Series{{Name: "Used", Pts: points(v, "mem")}}, now, metricTint(c, "memory"))
		if v.Info.OS == "darwin" {
			procSection(c, v, "Memory by app", func() {
				apps := byApp(v.Procs)
				table(c, []column{{title: "App"}, {title: "Processes", width: 9 * g(), end: true}, {title: "Memory", width: 10 * g(), end: true}},
					len(apps), func(r, col int) {
						switch col {
						case 0:
							text(c, apps[r].name, false)
						case 1:
							text(c, fmt.Sprint(apps[r].procs), true)
						case 2:
							text(c, format.Bytes(apps[r].rss), true)
						}
					})
			})
		}
		procSection(c, v, "Top processes by memory", func() {
			// RSS 0 means unreadable (macOS), not an empty process.
			top := topProcs(v.Procs, func(p model.Process) bool { return p.Mem() > 0 }, func(p model.Process) float64 { return float64(p.Mem()) })
			table(c, []column{{title: "Name"}, {title: "PID", width: 8 * g(), end: true}, {title: "Memory", width: 10 * g(), end: true}},
				len(top), func(r, col int) {
					switch col {
					case 0:
						text(c, top[r].Name, false)
					case 1:
						text(c, pid(top[r]), true)
					case 2:
						text(c, format.Bytes(top[r].Mem()), true)
					}
				})
		})
	})
}

// app is the processes of one application bundle (or of one executable name
// when it has no bundle) and their combined resident memory.
type app struct {
	name   string
	bundle string // "" for a plain executable
	procs  int
	rss    uint64
}

// byApp groups processes by application, falling back to Name for
// processes outside a bundle, and returns the topN groups by memory
// (footprint where known, else RSS). Helpers nested in an app (Chrome's
// renderers, under Google Chrome.app/…/Helpers/) count toward that app.
// Processes with no readable memory are skipped. A bundle is shown by its
// file name without ".app".
func byApp(ps []model.Process) []app {
	idx := map[string]int{}
	var apps []app
	for _, p := range ps {
		m := p.Mem()
		if m == 0 {
			continue
		}
		key, name, bundle := p.Name, p.Name, ""
		if p.AppBundle != "" {
			bundle = outerBundle(p.AppBundle)
			key = bundle
			name = strings.TrimSuffix(path.Base(bundle), ".app")
		}
		i, ok := idx[key]
		if !ok {
			i = len(apps)
			idx[key] = i
			apps = append(apps, app{name: name, bundle: bundle})
		}
		apps[i].procs++
		apps[i].rss += m
	}
	slices.SortStableFunc(apps, func(a, b app) int {
		switch {
		case a.rss > b.rss:
			return -1
		case a.rss < b.rss:
			return 1
		}
		return 0
	})
	if len(apps) > topN {
		apps = apps[:topN]
	}
	return apps
}

// outerBundle is the outermost *.app directory in a bundle path:
// "/Applications/A.app/Contents/Helpers/B.app" → "/Applications/A.app".
func outerBundle(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.Index(p, ".app/"); i >= 0 {
		return p[:i+len(".app")]
	}
	return p
}
