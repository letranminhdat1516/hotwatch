//go:build darwin

package darwin

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ebitengine/purego"
	gproc "github.com/shirou/gopsutil/v4/process"

	"hotwatch/model"
)

const maxCommandBytes = 4096

// procKey is the process identity: a PID alone is reused.
type procKey struct {
	pid   int32
	start int64 // start time, unix ms
}

type procPrev struct {
	cpuSecs     float64
	read, write uint64
	haveIO      bool
	at          time.Time
}

// Processes lists processes. Fields the OS refuses to give for other users'
// processes are unknown: Command "", Name falls back to the executable
// basename or "?", disk rates are nil. RSS is 0 when unreadable; UI code must
// not rank such processes in memory top-lists (RSS == 0 means unknown).
// Callers serialize Sample/Processes; the background lsof refresher is
// internally synchronized (portCache.mu) and never blocks this call after the
// first result exists.
// CPUPct is 0 on first sight of a (pid, start time). Environment is never read.
func (c *collector) Processes(ctx context.Context) ([]model.Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := c.now()
	list, err := gproc.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	ports := c.listeningPorts(ctx, now)

	results := make([]*procResult, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, gp := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			results[i] = readProc(ctx, gp, now)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	next := make(map[procKey]procPrev, len(list))
	out := make([]model.Process, 0, len(list))
	for _, r := range results {
		if r == nil {
			continue
		}
		prev, seen := c.prevProc[r.key]
		cur := r.prev
		if seen {
			dt := now.Sub(prev.at)
			if dt > 0 && cur.cpuSecs >= prev.cpuSecs {
				r.p.CPUPct = (cur.cpuSecs - prev.cpuSecs) / dt.Seconds() * 100
			}
			if cur.haveIO && prev.haveIO {
				r.p.DiskReadBps = model.Rate(prev.read, cur.read, dt)
				r.p.DiskWriteBps = model.Rate(prev.write, cur.write, dt)
			}
		}
		next[r.key] = cur
		r.p.Ports = ports[r.p.PID]
		out = append(out, r.p)
	}
	c.prevProc = next
	return out, nil
}

func readProc(ctx context.Context, gp *gproc.Process, now time.Time) *procResult {
	ct, err := gp.CreateTimeWithContext(ctx)
	if err != nil {
		return nil // exited, or not identifiable: no (PID, StartTime) identity
	}
	p := model.Process{PID: gp.Pid, StartTime: time.UnixMilli(ct)}
	if ppid, err := gp.PpidWithContext(ctx); err == nil {
		p.PPID = ppid
	}
	exe, _ := gp.ExeWithContext(ctx)
	p.AppBundle = appBundle(exe)
	if name, err := gp.NameWithContext(ctx); err == nil && name != "" {
		p.Name = name
	} else if exe != "" {
		p.Name = filepath.Base(exe)
	} else {
		p.Name = "?"
	}
	if u, err := gp.UsernameWithContext(ctx); err == nil {
		p.User = u
	}
	if cmd, err := gp.CmdlineWithContext(ctx); err == nil {
		p.Command = capCommand(cmd)
	}
	var cur procPrev
	cur.at = now
	if mi, err := gp.MemoryInfoWithContext(ctx); err == nil {
		p.RSS = mi.RSS
	}
	if n, err := gp.NumThreadsWithContext(ctx); err == nil {
		p.Threads = n
	}
	if t, err := gp.TimesWithContext(ctx); err == nil {
		cur.cpuSecs = t.User + t.System
	}
	if r, w, ok := diskIO(gp.Pid); ok {
		cur.read, cur.write, cur.haveIO = r, w, true
	}
	return &procResult{p: p, key: procKey{gp.Pid, ct}, prev: cur}
}

type procResult struct {
	p    model.Process
	key  procKey
	prev procPrev
}

// appBundle returns the nearest enclosing *.app directory of an executable
// path, or "".
func appBundle(exe string) string {
	for i := len(exe); i > 0; {
		j := strings.LastIndexByte(exe[:i], '/')
		if j < 0 {
			break
		}
		seg := exe[j+1 : i]
		if strings.HasSuffix(seg, ".app") && len(seg) > 4 && i < len(exe) {
			return exe[:i]
		}
		i = j
	}
	return ""
}

// capCommand truncates to maxCommandBytes without splitting a UTF-8 rune.
func capCommand(s string) string {
	if len(s) <= maxCommandBytes {
		return s
	}
	s = s[:maxCommandBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// --- proc_pid_rusage via purego (no cgo) ---

const (
	rusageInfoV4      = 4
	rusageBufSize     = 512 // rusage_info_v4 is 296 bytes
	offDiskBytesRead  = 144 // ri_diskio_bytesread in <sys/resource.h> rusage_info_v4
	offDiskBytesWrite = 152 // ri_diskio_byteswritten
)

var (
	rusageOnce sync.Once
	rusageFn   func(pid int32, flavor int32, buf *byte) int32
)

func loadRusage() {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	var fn func(pid int32, flavor int32, buf *byte) int32
	purego.RegisterLibFunc(&fn, lib, "proc_pid_rusage")
	rusageFn = fn
}

// diskIO returns cumulative bytes read/written; ok is false when the call
// fails (EPERM for other users' processes).
func diskIO(pid int32) (read, write uint64, ok bool) {
	rusageOnce.Do(loadRusage)
	if rusageFn == nil {
		return 0, 0, false
	}
	var buf [rusageBufSize]byte
	if rusageFn(pid, rusageInfoV4, &buf[0]) != 0 {
		return 0, 0, false
	}
	return leU64(buf[offDiskBytesRead:]), leU64(buf[offDiskBytesWrite:]), true
}

func leU64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}
