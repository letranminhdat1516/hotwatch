package portable

import (
	"context"
	"net"
	"slices"
	"sort"
	"strings"
	"time"

	"hotwatch/model"
)

type netCounters struct{ rx, tx uint64 }

type netState struct {
	prev   map[string]netCounters
	prevAt time.Time
}

// sampleNet lists the interfaces (loopback excluded) with their kind,
// addresses and rates. Rates are nil on the first sample and after a
// counter reset.
func (c *collector) sampleNet(ctx context.Context, now time.Time, out *[]model.Interface) bool {
	ifs, err := c.api.netIfs(ctx)
	if err != nil {
		return false
	}
	counters := map[string]netCounters{}
	if io, err := c.api.netIO(ctx); err == nil {
		for _, s := range io {
			counters[s.Name] = netCounters{s.BytesRecv, s.BytesSent}
		}
	}
	st := &c.net
	dt := now.Sub(st.prevAt)
	list := make([]model.Interface, 0, len(ifs))
	for _, f := range ifs {
		if slices.Contains(f.Flags, "loopback") || strings.HasPrefix(strings.ToLower(f.Name), "loopback") {
			continue
		}
		it := model.Interface{Name: f.Name, Up: slices.Contains(f.Flags, "up"), MAC: f.HardwareAddr}
		it.Kind, it.Label = kindOf(f.Name)
		for _, a := range f.Addrs {
			ip := a.Addr
			if p, _, err := net.ParseCIDR(a.Addr); err == nil {
				ip = p.String()
			}
			it.Addrs = append(it.Addrs, ip)
		}
		if cur, ok := counters[f.Name]; ok {
			rx, tx := cur.rx, cur.tx
			it.RxBytes, it.TxBytes = &rx, &tx
			if prev, had := st.prev[f.Name]; had {
				it.RxBps = model.Rate(prev.rx, cur.rx, dt)
				it.TxBps = model.Rate(prev.tx, cur.tx, dt)
			}
		}
		list = append(list, it)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	st.prev, st.prevAt = counters, now
	*out = list
	return true
}

// kindOf guesses an interface's kind from its name: Windows names them
// "Wi-Fi", "Ethernet 2", "vEthernet (WSL)"; the BSDs "wlan0", "em0", "tun0".
func kindOf(name string) (kind, label string) {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "wi-fi"), strings.Contains(n, "wireless"), strings.HasPrefix(n, "wlan"), strings.HasPrefix(n, "wl"):
		return "wifi", "Wi-Fi"
	case strings.Contains(n, "tailscale"), strings.Contains(n, "wireguard"), strings.Contains(n, "vpn"),
		strings.HasPrefix(n, "tun"), strings.HasPrefix(n, "wg"), strings.HasPrefix(n, "utun"):
		return "vpn", "VPN"
	case strings.HasPrefix(n, "vethernet"), strings.Contains(n, "hyper-v"), strings.Contains(n, "docker"),
		strings.HasPrefix(n, "bridge"), strings.HasPrefix(n, "br-"), strings.HasPrefix(n, "veth"):
		return "bridge", ""
	case strings.Contains(n, "ethernet"), strings.HasPrefix(n, "eth"), strings.HasPrefix(n, "en"),
		strings.HasPrefix(n, "em"), strings.HasPrefix(n, "igb"), strings.HasPrefix(n, "re"), strings.HasPrefix(n, "vtnet"):
		return "ethernet", "Ethernet"
	}
	return "", ""
}
