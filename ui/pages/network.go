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
	"hotwatch/ui/icons"
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

// Upload blue; download takes the network teal.
var uploadHex = "#0a84ff"

// Network builds the Network page (the owner's reference): download and
// upload with the totals since startup and the window's peak over a
// stacked dot chart, a card per connected interface, the apps using the
// most network and the listening ports.
func Network(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	pageColumn(c, func() {
		networkCard(c, v, now)
		if partial(s, "net") {
			return
		}
		ifs := shownInterfaces(s.Net)
		if len(ifs) > 0 {
			ui.Grid(c).Columns(3).Gap(2 * g()).Children(func() {
				for i, it := range ifs {
					interfaceCard(c, it, ifaceTitle(ifs, i))
				}
			})
		}
		if len(v.Procs) == 0 {
			note(c, noProcs)
			return
		}
		topNetworkCard(c, v)
		portsCard(c, v)
	})
}

// counted are the interfaces whose traffic is the host's own: tunnels and
// bridges carry bytes a physical interface counts too (as History counts).
func counted(ifs []model.Interface) []model.Interface {
	var out []model.Interface
	for _, i := range ifs {
		if i.Up && i.Name != "lo" && i.Name != "lo0" && i.Kind != "vpn" && i.Kind != "bridge" {
			out = append(out, i)
		}
	}
	return out
}

// networkCard is the download rate, upload, totals since startup and the
// window's peak, and the chart: upload under download.
func networkCard(c *ui.Context, v hub.HostView, now time.Time) {
	s := v.Snap
	down, up := metricTint(c, "network"), ui.Hex(uploadHex)
	ifs := counted(s.Net)
	from := now.Add(-components.ChartWindow())
	right := func() {
		legend(c, down, "Download")
		legend(c, up, "Upload")
	}
	pageCard(c, icons.Network, "Network", "network", right, func() {
		rx, okR := sumPtr(ifs, func(i model.Interface) *float64 { return i.RxBps })
		tx, okT := sumPtr(ifs, func(i model.Interface) *float64 { return i.TxBps })
		val, unit := format.Unknown, ""
		if okR && !partial(s, "net") {
			val, unit = splitUnit(format.Rate(&rx))
		}
		figs := []fig{{"Uploading", rateOr(tx, okT), ""}}
		if t, ok := sumIfBytes(ifs, func(i model.Interface) *uint64 { return i.RxBytes }); ok {
			figs = append(figs, fig{"Received", format.Bytes(t), "since startup"})
		}
		if t, ok := sumIfBytes(ifs, func(i model.Interface) *uint64 { return i.TxBytes }); ok {
			figs = append(figs, fig{"Sent", format.Bytes(t), "since startup"})
		}
		if v.Hist != nil {
			if _, p, ok := v.Hist.Stats("net.rxtx", from); ok {
				figs = append(figs, fig{"Peak", format.Rate(&p), ""})
			}
		}
		headline(c, "Downloading", val, unit, figs)
		layers := []components.DotLayer{{Pts: points(v, "net.tx"), Color: up}}
		if rt := points(v, "net.rxtx"); len(rt) > 0 {
			layers = append(layers, components.DotLayer{Pts: rt, Color: down})
		} else {
			layers = []components.DotLayer{{Pts: points(v, "net.rx"), Color: down}}
		}
		components.StackedDotChart(c, layers, from, now, 0, down, 200).Margin(g(), 0, 0, 0)
	})
}

// shownInterfaces are the connected interfaces worth a card: up, with an
// address beyond the link-local ones, not a bridge; Wi-Fi and Ethernet
// first, then VPNs, each by name.
func shownInterfaces(ifs []model.Interface) []model.Interface {
	var out []model.Interface
	for _, i := range ifs {
		if !i.Up || i.Kind == "bridge" || i.Name == "lo" || i.Name == "lo0" {
			continue
		}
		if len(globalAddrs(i.Addrs)) == 0 {
			continue
		}
		out = append(out, i)
	}
	rank := func(k string) int {
		switch k {
		case "wifi", "ethernet":
			return 0
		case "vpn":
			return 2
		}
		return 1
	}
	slices.SortStableFunc(out, func(a, b model.Interface) int {
		return cmp.Or(cmp.Compare(rank(a.Kind), rank(b.Kind)), strings.Compare(a.Name, b.Name))
	})
	return out
}

// globalAddrs drops link-local addresses (fe80::, 169.254.) from addrs.
func globalAddrs(addrs []string) []string {
	var out []string
	for _, a := range addrs {
		if strings.HasPrefix(a, "fe80:") || strings.HasPrefix(a, "169.254.") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// ifaceTitle is interface i's card title: the system's name for it, or
// what it is; a second VPN is "VPN 2".
func ifaceTitle(ifs []model.Interface, i int) string {
	it := ifs[i]
	title := it.Label
	if title == "" {
		switch it.Kind {
		case "wifi":
			title = "Wi-Fi"
		case "ethernet":
			title = "Ethernet"
		case "vpn":
			title = "VPN"
		default:
			title = it.Name
		}
	}
	n := 1
	for j := 0; j < i; j++ {
		if ifaceLabel(ifs[j]) == ifaceLabel(it) {
			n++
		}
	}
	if n > 1 {
		title += fmt.Sprintf(" %d", n)
	}
	return title
}

func ifaceLabel(i model.Interface) string { return i.Kind + "/" + i.Label }

// interfaceCard is one interface's traffic, totals and addresses.
func interfaceCard(c *ui.Context, it model.Interface, title string) {
	t := c.Theme()
	icon, key := icons.Network, "network"
	switch it.Kind {
	case "wifi":
		icon = icons.Wifi
	case "vpn":
		icon, key = icons.Shield, "cpu" // VPNs in blue, as the reference shows them
	}
	right := func() {
		ui.Text(c, it.Name).FontSize(13).Font("monospace").TextColor(t.TextMuted).SingleLine()
	}
	pageCard(c, icon, title, key, right, func() {
		val, unit := splitUnit(format.Rate(it.RxBps))
		headline(c, "Downloading", val, unit, nil)
		bytesOr := func(b *uint64) string {
			if b == nil {
				return format.Unknown
			}
			return format.Bytes(*b)
		}
		statGrid(c, 3, []fig{{"Uploading", format.Rate(it.TxBps), ""}, {"Received", bytesOr(it.RxBytes), ""}, {"Sent", bytesOr(it.TxBytes), ""}})
		ui.Column(c).Gap(4).Margin(g(), 0, 0, 0).Children(func() {
			var v4, v6 string
			for _, a := range it.Addrs {
				ip, _, _ := strings.Cut(a, "/")
				switch {
				case strings.Contains(ip, ":") && v6 == "":
					v6 = ip
				case strings.Contains(ip, ":") && strings.HasPrefix(v6, "fe80:"):
					v6 = ip // a global IPv6 address over the link-local one
				case !strings.Contains(ip, ":") && v4 == "":
					v4 = ip
				}
			}
			if v4 != "" {
				kvRow(c, "IPv4", v4)
			}
			if v6 != "" {
				kvRow(c, "IPv6", v6)
			}
			if it.MAC != "" {
				kvRow(c, "Hardware", it.MAC)
			}
		})
	})
}

// topNetworkCard lists the apps sending and receiving the most.
func topNetworkCard(c *ui.Context, v hub.HostView) {
	if len(v.Procs) == 0 {
		return
	}
	apps := groupByApp(v.Procs, func(p model.Process) (float64, bool) {
		if p.NetRxBps == nil && p.NetTxBps == nil {
			return 0, false
		}
		var s float64
		for _, r := range []*float64{p.NetRxBps, p.NetTxBps} {
			if r != nil {
				s += *r
			}
		}
		return s, true
	})
	known := false
	for _, p := range v.Procs {
		known = known || p.NetRxBps != nil
	}
	if !known {
		return // the host does not report per-process traffic
	}
	tint := metricTint(c, "network")
	pageCard(c, icons.Network, "Using the Most Network", "network", func() { showAll(c, "net-show-all", "processes", tint) }, func() {
		if len(apps) == 0 {
			note(c, "No network traffic right now.")
			return
		}
		topApps(c, apps, 10, func(b float64) string { return format.Rate(&b) })
	})
}

// portsCard lists the listening ports and who holds them.
func portsCard(c *ui.Context, v hub.HostView) {
	ls := listeners(v.Procs)
	if len(ls) == 0 {
		return
	}
	pageCard(c, icons.Globe, "Listening Ports", "", func() {
		ui.Text(c, plural(len(ls), "port")).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine()
	}, func() {
		ui.Grid(c).Columns(2).GapX(6 * g()).GapY(g()).Children(func() {
			for i, l := range ls[:min(len(ls), 20)] {
				ui.Row(c).Key(fmt.Sprintf("port-%d", i)).Gap(g()).Children(func() {
					ui.Text(c, fmt.Sprintf("%d/%s", l.port, l.proto)).FontSize(14).FontWeight(600).Width(12 * g()).SingleLine()
					ui.Text(c, l.name).FontSize(14).SingleLine().Grow(1)
					ui.Text(c, fmt.Sprint(l.pid)).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine()
				})
			}
		})
	})
}

// sumIfBytes adds the known byte counts of ifs; ok is false when none is known.
func sumIfBytes(ifs []model.Interface, f func(model.Interface) *uint64) (uint64, bool) {
	var s uint64
	ok := false
	for _, i := range ifs {
		if b := f(i); b != nil {
			s += *b
			ok = true
		}
	}
	return s, ok
}
