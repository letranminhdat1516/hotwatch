//go:build darwin

package darwin

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ebitengine/purego"
	gproc "github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"

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
	energy      uint64 // energy used, nJ
	haveIO      bool
	net         netBytes // nettop's counters
	haveNet     bool
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
	// One sysctl lists every process with its identity (start time, command
	// name, uid) and parent.
	list, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	ports := c.listeningPorts(ctx, now)
	var net map[int32]netBytes
	if c.nettop != nil {
		if data, err := c.nettop(ctx); err == nil {
			net = parseNettop(data)
		}
	}

	results := make([]*procResult, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			results[i] = readProc(ctx, &list[i], now, c.statics)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	next := make(map[procKey]procPrev, len(list))
	statics := make(map[procKey]procStatic, len(list))
	out := make([]model.Process, 0, len(list))
	for _, r := range results {
		if r == nil {
			continue
		}
		prev, seen := c.prevProc[r.key]
		cur := r.prev
		cur.net, cur.haveNet = net[r.p.PID]
		if seen && cur.haveNet && prev.haveNet {
			dt := now.Sub(prev.at)
			r.p.NetRxBps = model.Rate(prev.net.in, cur.net.in, dt)
			r.p.NetTxBps = model.Rate(prev.net.out, cur.net.out, dt)
		}
		if seen {
			dt := now.Sub(prev.at)
			if dt > 0 && cur.cpuSecs >= prev.cpuSecs {
				r.p.CPUPct = (cur.cpuSecs - prev.cpuSecs) / dt.Seconds() * 100
			}
			if cur.haveIO && prev.haveIO {
				r.p.DiskReadBps = model.Rate(prev.read, cur.read, dt)
				r.p.DiskWriteBps = model.Rate(prev.write, cur.write, dt)
				if nj := model.Rate(prev.energy, cur.energy, dt); nj != nil {
					mw := *nj / 1e6 // nJ/s → mW
					r.p.PowerMW = &mw
				}
			}
		}
		next[r.key] = cur
		statics[r.key] = r.static
		r.p.Ports = ports[r.p.PID]
		out = append(out, r.p)
	}
	c.prevProc, c.statics = next, statics // exited processes drop out of both
	return out, nil
}

// procStatic are the facts of a process that do not change while it runs
// the same program: read once and reused by later scans while the PID, the
// start time, the kernel's command name and the uid stay the same. exec()
// keeps the PID and start time but changes the command name; a setuid exec
// changes the uid; either reads the facts again.
type procStatic struct {
	name, user, command, appBundle string
	comm                           string // kinfo_proc p_comm when read
	uid                            uint32 // kinfo_proc effective uid when read
}

// staticReads counts full reads of static facts (tests).
var staticReads atomic.Int64

func readStatic(ctx context.Context, gp *gproc.Process) procStatic {
	staticReads.Add(1)
	var st procStatic
	exe, _ := gp.ExeWithContext(ctx)
	st.appBundle = appBundle(exe)
	if name, err := gp.NameWithContext(ctx); err == nil && name != "" {
		st.name = name
	} else if exe != "" {
		st.name = filepath.Base(exe)
	} else {
		st.name = "?"
	}
	st.name = programName(st.name, exe)
	if u, err := gp.UsernameWithContext(ctx); err == nil {
		st.user = u
	}
	if cmd, err := gp.CmdlineWithContext(ctx); err == nil {
		st.command = capCommand(cmd)
	}
	return st
}

// readProc reads one process from its kinfo_proc. statics holds the static
// facts of processes seen before (read-only here); a process not in it is
// read in full.
func readProc(ctx context.Context, kp *unix.KinfoProc, now time.Time, statics map[procKey]procStatic) *procResult {
	pid := kp.Proc.P_pid
	if pid < 0 {
		return nil
	}
	ct := kp.Proc.P_starttime.Sec*1000 + int64(kp.Proc.P_starttime.Usec)/1000 // as gopsutil's CreateTime
	comm := cString(kp.Proc.P_comm[:])
	uid := kp.Eproc.Ucred.Uid
	key := procKey{pid, ct}
	st, ok := statics[key]
	if !ok || st.comm != comm || st.uid != uid {
		st = readStatic(ctx, &gproc.Process{Pid: pid})
		st.comm, st.uid = comm, uid
	}
	p := model.Process{PID: pid, PPID: kp.Eproc.Ppid, StartTime: time.UnixMilli(ct),
		Name: st.name, User: st.user, Command: st.command, AppBundle: st.appBundle}
	var cur procPrev
	cur.at = now
	if ti, ok := taskInfo(pid); ok {
		p.RSS = ti.rss
		p.Threads = ti.threads
		cur.cpuSecs = ti.cpuSecs
	}
	if fp, r, w, e, ok := rusage(pid); ok {
		p.Footprint = fp
		cur.read, cur.write, cur.energy, cur.haveIO = r, w, e, true
	}
	return &procResult{p: p, key: key, prev: cur, static: st}
}

// cString is a NUL-terminated byte array as a string.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

type procResult struct {
	p      model.Process
	key    procKey
	prev   procPrev
	static procStatic
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
	rusageBufSize     = 512 // rusage_info_v6 is 464 bytes
	offFootprint      = 72  // ri_phys_footprint in <sys/resource.h> rusage_info_v4
	offDiskBytesRead  = 144 // ri_diskio_bytesread
	rusageInfoV6      = 6   // adds ri_energy_nj (macOS 14+)
	offEnergy         = 336 // ri_energy_nj in rusage_info_v6, nanojoules
	offDiskBytesWrite = 152 // ri_diskio_byteswritten
)

var (
	rusageOnce sync.Once
	rusageFn   func(pid int32, flavor int32, buf *byte) int32
	pidInfoFn  func(pid, flavor int32, arg uint64, buf *byte, size int32) int32
	tickNanos  float64 // mach absolute time unit in ns
)

// loadRusage loads proc_pid_rusage and proc_pidinfo once; gopsutil opens
// libSystem again on every call.
func loadRusage() {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return
	}
	var fn func(pid int32, flavor int32, buf *byte) int32
	purego.RegisterLibFunc(&fn, lib, "proc_pid_rusage")
	rusageFn = fn
	var info func(pid, flavor int32, arg uint64, buf *byte, size int32) int32
	purego.RegisterLibFunc(&info, lib, "proc_pidinfo")
	var timebase func(info *[2]uint32) int32
	purego.RegisterLibFunc(&timebase, lib, "mach_timebase_info")
	var tb [2]uint32 // numer, denom
	if timebase(&tb) == 0 && tb[1] != 0 {
		pidInfoFn, tickNanos = info, float64(tb[0])/float64(tb[1])
	}
}

// proc_taskinfo (<sys/proc_info.h>), flavor PROC_PIDTASKINFO.
const (
	procPidTaskInfo = 4
	taskInfoSize    = 96
	offResidentSize = 8
	offTotalUser    = 16 // mach absolute time units
	offTotalSystem  = 24
	offThreadNum    = 84
)

type task struct {
	rss     uint64
	threads int32
	cpuSecs float64
}

// taskInfo reads the resident size, thread count and CPU time in one
// proc_pidinfo call; ok is false when it fails (EPERM for other users'
// processes).
func taskInfo(pid int32) (t task, ok bool) {
	rusageOnce.Do(loadRusage)
	if pidInfoFn == nil {
		return t, false
	}
	var buf [taskInfoSize]byte
	if pidInfoFn(pid, procPidTaskInfo, 0, &buf[0], taskInfoSize) != taskInfoSize {
		return t, false
	}
	ticks := leU64(buf[offTotalUser:]) + leU64(buf[offTotalSystem:])
	return task{
		rss:     leU64(buf[offResidentSize:]),
		threads: int32(uint32(buf[offThreadNum]) | uint32(buf[offThreadNum+1])<<8 | uint32(buf[offThreadNum+2])<<16 | uint32(buf[offThreadNum+3])<<24),
		cpuSecs: float64(ticks) * tickNanos / 1e9,
	}, true
}

// rusage reads proc_pid_rusage (v6, else v4): the physical footprint,
// cumulative bytes read/written and energy used (0 before v6); ok is false
// when the call fails (EPERM for other users' processes).
func rusage(pid int32) (footprint, read, write, energy uint64, ok bool) {
	rusageOnce.Do(loadRusage)
	if rusageFn == nil {
		return 0, 0, 0, 0, false
	}
	var buf [rusageBufSize]byte
	if rusageFn(pid, rusageInfoV6, &buf[0]) == 0 {
		return leU64(buf[offFootprint:]), leU64(buf[offDiskBytesRead:]), leU64(buf[offDiskBytesWrite:]), leU64(buf[offEnergy:]), true
	}
	if rusageFn(pid, rusageInfoV4, &buf[0]) != 0 { // older macOS: no energy
		return 0, 0, 0, 0, false
	}
	return leU64(buf[offFootprint:]), leU64(buf[offDiskBytesRead:]), leU64(buf[offDiskBytesWrite:]), 0, true
}

func leU64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// versionRe matches a file named after a version, "2.1.296".
var versionRe = regexp.MustCompile(`^v?\d+(\.\d+)+$`)

// programName is the name to show for a process: its own, unless that is a
// version number, as for tools that install each version under its own
// file name (~/.local/share/claude/versions/2.1.296); then the nearest
// folder above it that names the program ("claude").
func programName(name, exe string) string {
	if !versionRe.MatchString(name) || exe == "" {
		return name
	}
	for dir := filepath.Dir(exe); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		switch b := filepath.Base(dir); {
		case b == "versions", b == "bin", versionRe.MatchString(b):
			continue
		default:
			return b
		}
	}
	return name
}
