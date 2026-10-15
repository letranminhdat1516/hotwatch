//go:build darwin

package darwin

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"

	"hotwatch/model"
)

type netCounters struct{ rx, tx uint64 }

// sampleNet lists interfaces (loopback and idle down interfaces excluded)
// with rx/tx rates. Rates are nil on the first sample and after a counter
// reset. It returns false when the counters are unreadable.
func (c *collector) sampleNet(ctx context.Context, now time.Time, out *[]model.Interface) bool {
	stats, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return false
	}
	cur := make(map[string]netCounters, len(stats))
	for _, s := range stats {
		cur[s.Name] = netCounters{rx: s.BytesRecv, tx: s.BytesSent}
	}
	ifaces := map[string]net.Interface{}
	if l, err := net.Interfaces(); err == nil {
		for _, i := range l {
			ifaces[i.Name] = i
		}
	}
	// Interface kinds change rarely: re-read them when the interface set does.
	if key := ifaceKey(cur); key != c.netKindsKey {
		c.netKinds, c.netKindsKey = readNetKinds(), key
	}
	dt := now.Sub(c.prevNetAt)
	names := make([]string, 0, len(cur))
	for n := range cur {
		names = append(names, n)
	}
	sort.Strings(names)
	var res []model.Interface
	for _, n := range names {
		i, known := ifaces[n]
		if i.Flags&net.FlagLoopback != 0 || n == "lo0" {
			continue
		}
		up := known && i.Flags&net.FlagUp != 0
		if !up && cur[n] == (netCounters{}) {
			continue
		}
		rx, tx := cur[n].rx, cur[n].tx
		k := interfaceKind(c.netKinds, n)
		it := model.Interface{Name: n, Up: up, Kind: k.kind, Label: k.label, RxBytes: &rx, TxBytes: &tx}
		if known && len(i.HardwareAddr) > 0 {
			it.MAC = i.HardwareAddr.String()
		}
		if known {
			if addrs, err := i.Addrs(); err == nil {
				for _, a := range addrs {
					it.Addrs = append(it.Addrs, a.String())
				}
			}
		}
		if prev, ok := c.prevNet[n]; ok {
			it.RxBps = model.Rate(prev.rx, cur[n].rx, dt)
			it.TxBps = model.Rate(prev.tx, cur[n].tx, dt)
		}
		res = append(res, it)
	}
	*out = res
	c.prevNet, c.prevNetAt = cur, now
	return true
}

// ifaceKey identifies a set of interfaces.
func ifaceKey(m map[string]netCounters) string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}
