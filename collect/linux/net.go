package linux

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"hotwatch/model"
)

type netCounters struct{ rx, tx uint64 }

// readNetDev maps interface name to cumulative rx/tx bytes.
func readNetDev(path string) (map[string]netCounters, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]netCounters{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue // header lines
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out[strings.TrimSpace(name)] = netCounters{rx: rx, tx: tx}
	}
	return out, sc.Err()
}

// sampleNet fills the interface list (loopback excluded). It returns false
// when /proc/net/dev is unreadable. Rates are nil on the first sample and
// after a counter reset.
func (c *collector) sampleNet(now time.Time, out *[]model.Interface) bool {
	cur, err := readNetDev(c.proc("net/dev"))
	if err != nil {
		return false
	}
	dt := now.Sub(c.prevNetAt)
	names := make([]string, 0, len(cur))
	for n := range cur {
		if n != "lo" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	ifs := make([]model.Interface, 0, len(names))
	for _, n := range names {
		rx, tx := cur[n].rx, cur[n].tx
		it := model.Interface{Name: n, Up: c.ifaceUp(n), RxBytes: &rx, TxBytes: &tx}
		switch {
		case strings.HasPrefix(n, "wl"):
			it.Kind, it.Label = "wifi", "Wi-Fi"
		case strings.HasPrefix(n, "tailscale"), strings.HasPrefix(n, "wg"), strings.HasPrefix(n, "tun"):
			it.Kind, it.Label = "vpn", "VPN"
		case strings.HasPrefix(n, "en"), strings.HasPrefix(n, "eth"):
			it.Kind, it.Label = "ethernet", "Ethernet"
		case strings.HasPrefix(n, "docker"), strings.HasPrefix(n, "br-"), strings.HasPrefix(n, "veth"), strings.HasPrefix(n, "cni"):
			it.Kind = "bridge"
		}
		if c.ifaddrs != nil {
			it.Addrs = c.ifaddrs(n)
		}
		if prev, had := c.prevNet[n]; had {
			it.RxBps = model.Rate(prev.rx, cur[n].rx, dt)
			it.TxBps = model.Rate(prev.tx, cur[n].tx, dt)
		}
		ifs = append(ifs, it)
	}
	*out = ifs
	c.prevNet, c.prevNetAt = cur, now
	return true
}

// ifaceUp reports operstate "up"; a missing file means not up.
func (c *collector) ifaceUp(name string) bool {
	b, err := os.ReadFile(c.root + "/sys/class/net/" + name + "/operstate")
	return err == nil && strings.TrimSpace(string(b)) == "up"
}
