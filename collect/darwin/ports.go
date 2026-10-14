//go:build darwin

package darwin

import (
	"bytes"
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hotwatch/model"
)

const (
	portsTTL    = 10 * time.Second
	lsofTimeout = 3 * time.Second
)

const (
	portsMaxAge    = 60 * time.Second // a last good map older than this is dropped
	portsFirstWait = 500 * time.Millisecond
)

// portCache holds the last good lsof result. Refreshes run in one background
// goroutine at a time so Processes() never waits for lsof (except the very
// first call, bounded by portsFirstWait). All fields are guarded by mu.
type portCache struct {
	mu       sync.Mutex
	ports    map[int32][]model.Port
	goodAt   time.Time
	haveGood bool
	lastTry  time.Time
	tried    bool
	running  bool
	first    chan struct{} // closed when the first refresh finishes
}

// runLsof lists listening TCP sockets and UDP sockets. lsof only sees
// processes the current user can inspect; others simply have no ports.
func runLsof(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, lsofTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-iUDP", "-F", "pcnPt").Output()
}

// listeningPorts returns pid -> ports from the last good lsof run. It starts a
// background refresh when the last attempt is older than portsTTL and none is
// running; the refresh uses its own context, never the caller's. A failed
// refresh keeps the previous good map (dropped only after portsMaxAge) and is
// retried after the normal interval. Safe for concurrent use.
func (c *collector) listeningPorts(_ context.Context, now time.Time) map[int32][]model.Port {
	pc := &c.ports
	pc.mu.Lock()
	if pc.first == nil {
		pc.first = make(chan struct{})
	}
	first := pc.first
	if !pc.running && (!pc.tried || now.Sub(pc.lastTry) >= portsTTL || now.Before(pc.lastTry)) {
		pc.running, pc.tried, pc.lastTry = true, true, now
		go c.refreshPorts(now)
	}
	waitFirst := !pc.haveGood
	pc.mu.Unlock()

	if waitFirst {
		t := time.NewTimer(portsFirstWait)
		select {
		case <-first:
		case <-t.C:
		}
		t.Stop()
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pc.haveGood && now.Sub(pc.goodAt) > portsMaxAge {
		pc.ports, pc.haveGood = nil, false
	}
	if !pc.haveGood {
		return map[int32][]model.Port{}
	}
	return pc.ports
}

func (c *collector) refreshPorts(startedAt time.Time) {
	run := c.lsof
	if run == nil {
		run = runLsof
	}
	out, err := run(context.Background())
	pc := &c.ports
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if err == nil || len(out) > 0 {
		pc.ports, pc.goodAt, pc.haveGood = parseLsofPorts(out), startedAt, true
	}
	pc.running = false
	select {
	case <-pc.first:
	default:
		close(pc.first)
	}
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
