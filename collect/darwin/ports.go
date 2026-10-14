//go:build darwin

package darwin

import (
	"bytes"
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"hotwatch/model"
)

const (
	portsTTL    = 10 * time.Second
	lsofTimeout = 3 * time.Second
)

// portCache holds the last lsof result so the 2 s process poll does not spawn
// lsof every time.
type portCache struct {
	at    time.Time
	ports map[int32][]model.Port
	ok    bool
}

// runLsof lists listening TCP sockets and UDP sockets. lsof only sees
// processes the current user can inspect; others simply have no ports.
func runLsof(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, lsofTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-iUDP", "-F", "pcnPt").Output()
}

// listeningPorts returns pid -> ports, cached for portsTTL. A missing lsof or
// a failure yields an empty map; it never fails the process list.
func (c *collector) listeningPorts(ctx context.Context, now time.Time) map[int32][]model.Port {
	if c.ports.ok && now.Sub(c.ports.at) < portsTTL && now.Sub(c.ports.at) >= 0 {
		return c.ports.ports
	}
	run := c.lsof
	if run == nil {
		run = runLsof
	}
	out, err := run(ctx)
	if err != nil && len(out) == 0 {
		// Missing lsof or a failure: cache "no ports" for the TTL so a slow
		// failure (3 s timeout) is not retried on every poll.
		c.ports = portCache{at: now, ports: map[int32][]model.Port{}, ok: true}
		return c.ports.ports
	}
	c.ports = portCache{at: now, ports: parseLsofPorts(out), ok: true}
	return c.ports.ports
}

// parseLsofPorts parses `lsof -F pcnPt` output. Each "p" line starts a
// process; each "t"/"P"/"n" group describes one socket. TCP entries are
// already filtered to LISTEN by lsof; UDP entries with a peer ("->") are
// connected sockets, not listeners, and are skipped.
func parseLsofPorts(out []byte) map[int32][]model.Port {
	res := map[int32][]model.Port{}
	var pid int32
	var proto string
	for _, line := range strings.Split(string(bytes.ReplaceAll(out, []byte("\r"), nil)), "\n") {
		if len(line) < 2 {
			continue
		}
		val := line[1:]
		switch line[0] {
		case 'p':
			n, err := strconv.ParseInt(val, 10, 32)
			if err != nil {
				pid = 0
			} else {
				pid = int32(n)
			}
			proto = ""
		case 'f':
			proto = ""
		case 'P':
			proto = strings.ToLower(val)
		case 'n':
			if pid == 0 || (proto != "tcp" && proto != "udp") || strings.Contains(val, "->") {
				continue
			}
			i := strings.LastIndexByte(val, ':')
			if i < 0 {
				continue
			}
			n, err := strconv.ParseUint(val[i+1:], 10, 16)
			if err != nil {
				continue
			}
			p := model.Port{Number: uint16(n), Proto: proto}
			dup := false
			for _, q := range res[pid] {
				if q == p {
					dup = true
				}
			}
			if !dup {
				res[pid] = append(res[pid], p)
			}
		}
	}
	for _, ps := range res {
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].Proto != ps[j].Proto {
				return ps[i].Proto < ps[j].Proto
			}
			return ps[i].Number < ps[j].Number
		})
	}
	return res
}
