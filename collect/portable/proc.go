package portable

import (
	"context"
	"time"

	"hotwatch/model"
)

// portsTTL is how long one listening-port listing is reused.
const portsTTL = 5 * time.Second

// procKey is a process identity: a PID alone is reused.
type procKey struct {
	pid    int32
	create int64 // start time, unix ms
}

// procStatic are the facts that do not change while a process runs.
type procStatic struct{ name, user, command string }

// procInfo is one process as the system reports it.
type procInfo struct {
	PID, PPID   int32
	Create      int64 // start time, unix ms
	Name, User  string
	Command     string
	RSS         uint64
	Threads     int32
	CPUSecs     float64 // user + system time
	Read, Write uint64  // bytes transferred
	HaveIO      bool
}

type procPrev struct {
	cpuSecs     float64
	read, write uint64
	haveIO      bool
	at          time.Time
}

type procState struct {
	prev    map[procKey]procPrev
	statics map[procKey]procStatic
	ports   map[int32][]model.Port
	portsAt time.Time
}

// Processes lists processes. CPUPct is 0 on first sight of a (PID, start
// time); disk rates are nil until two readings exist.
func (c *collector) Processes(ctx context.Context) ([]model.Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := c.api.now()
	st := &c.procs
	list, err := c.api.procs(ctx, st.statics)
	if err != nil {
		return nil, err
	}
	if c.api.listeners != nil && (st.ports == nil || now.Sub(st.portsAt) >= portsTTL) {
		if ports, err := c.api.listeners(ctx); err == nil {
			st.ports, st.portsAt = ports, now
		}
	}
	next := make(map[procKey]procPrev, len(list))
	statics := make(map[procKey]procStatic, len(list))
	out := make([]model.Process, 0, len(list))
	for _, pi := range list {
		key := procKey{pi.PID, pi.Create}
		p := model.Process{PID: pi.PID, PPID: pi.PPID, StartTime: time.UnixMilli(pi.Create),
			Name: pi.Name, User: pi.User, Command: pi.Command, RSS: pi.RSS, Threads: pi.Threads,
			Ports: st.ports[pi.PID]}
		cur := procPrev{cpuSecs: pi.CPUSecs, read: pi.Read, write: pi.Write, haveIO: pi.HaveIO, at: now}
		if prev, seen := st.prev[key]; seen {
			dt := now.Sub(prev.at)
			if dt > 0 && cur.cpuSecs >= prev.cpuSecs {
				p.CPUPct = (cur.cpuSecs - prev.cpuSecs) / dt.Seconds() * 100
			}
			if cur.haveIO && prev.haveIO {
				p.DiskReadBps = model.Rate(prev.read, cur.read, dt)
				p.DiskWriteBps = model.Rate(prev.write, cur.write, dt)
			}
		}
		next[key] = cur
		statics[key] = procStatic{pi.Name, pi.User, pi.Command}
		out = append(out, p)
	}
	st.prev, st.statics = next, statics // exited processes drop out of both
	return out, nil
}
