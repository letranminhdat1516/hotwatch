package pages

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// listener is one listening socket and the process that holds it.
type listener struct {
	port  uint16
	proto string
	name  string
	pid   int32
}

// listeners are the listening ports of ps, by port, then protocol, then PID;
// a process listening twice on one port and protocol is listed once.
func listeners(ps []model.Process) []listener {
	var out []listener
	for _, p := range ps {
		for _, po := range p.Ports {
			out = append(out, listener{po.Number, po.Proto, p.Name, p.PID})
		}
	}
	slices.SortFunc(out, func(a, b listener) int {
		return cmp.Or(cmp.Compare(a.port, b.port), strings.Compare(a.proto, b.proto), cmp.Compare(a.pid, b.pid))
	})
	return slices.Compact(out)
}

// Network builds the Network page: the interfaces, inbound and outbound
// throughput over the last five minutes and the listening ports.
func Network(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	pageColumn(c, func() {
		section(c, "Interfaces", func() {
			if partial(s, "net") {
				note(c, format.Unknown)
				return
			}
			var ifs []model.Interface
			for _, i := range s.Net {
				if i.Name != "lo" && i.Name != "lo0" {
					ifs = append(ifs, i)
				}
			}
			if len(ifs) == 0 {
				note(c, format.Unknown)
				return
			}
			table(c, []column{{title: "Interface", width: 10 * g()}, {title: "Addresses"}, {title: "Status", width: 10 * g()},
				{title: "In", width: 11 * g(), end: true}, {title: "Out", width: 11 * g(), end: true}},
				len(ifs), func(r, col int) {
					i := ifs[r]
					switch col {
					case 0:
						text(c, i.Name, false)
					case 1:
						addrs := strings.Join(i.Addrs, ", ")
						if addrs == "" {
							addrs = format.Unknown
						}
						text(c, addrs, false)
					case 2:
						state := "down"
						if i.Up {
							state = "up"
						}
						ui.Row(c).Children(func() { components.Badge(c, state) })
					case 3:
						text(c, format.Rate(i.RxBps), true)
					case 4:
						text(c, format.Rate(i.TxBps), true)
					}
				})
		})
		components.DotTimeChart(c, "Network throughput", "B/s", []components.Series{
			{Name: "In", Pts: points(v, "net.rx")}, {Name: "Out", Pts: points(v, "net.tx")}}, now, metricTint(c, "network"))
		procSection(c, v, "Listening ports", func() {
			ls := listeners(v.Procs)
			if len(ls) == 0 {
				note(c, "No listening ports")
				return
			}
			table(c, []column{{title: "Port", width: 12 * g()}, {title: "Process"}, {title: "PID", width: 8 * g(), end: true}},
				len(ls), func(r, col int) {
					switch col {
					case 0:
						text(c, fmt.Sprintf("%d/%s", ls[r].port, ls[r].proto), false)
					case 1:
						text(c, ls[r].name, false)
					case 2:
						text(c, fmt.Sprint(ls[r].pid), true)
					}
				})
		})
	})
}
