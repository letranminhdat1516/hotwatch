package portable

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	gproc "github.com/shirou/gopsutil/v4/process"

	"hotwatch/model"
)

// maxCommandBytes caps a command line; the rest is cut.
const maxCommandBytes = 4096

func systemAPI() api {
	a := api{
		now:      time.Now,
		cpuTimes: func(ctx context.Context) ([]cpu.TimesStat, error) { return cpu.TimesWithContext(ctx, true) },
		vmem:     mem.VirtualMemoryWithContext,
		swap:     mem.SwapMemoryWithContext,
		uptime:   host.UptimeWithContext,
		partitions: func(ctx context.Context) ([]disk.PartitionStat, error) {
			return disk.PartitionsWithContext(ctx, false)
		},
		usage: disk.UsageWithContext,
		diskIO: func(ctx context.Context) (map[string]disk.IOCountersStat, error) {
			return disk.IOCountersWithContext(ctx)
		},
		netIO:     func(ctx context.Context) ([]gnet.IOCountersStat, error) { return gnet.IOCountersWithContext(ctx, true) },
		netIfs:    gnet.InterfacesWithContext,
		procs:     readProcs,
		listeners: readListeners,
	}
	if runtime.GOOS != "windows" { // Windows has no load average
		a.load = func(ctx context.Context) ([3]float64, error) {
			l, err := load.AvgWithContext(ctx)
			if err != nil {
				return [3]float64{}, err
			}
			return [3]float64{l.Load1, l.Load5, l.Load15}, nil
		}
	}
	return a
}

// bootMillis is the boot time in unix ms, the start time given to a
// process whose own the system will not tell (Windows' System and Idle).
var bootMillis = sync.OnceValue(func() int64 {
	b, err := host.BootTime()
	if err != nil {
		return 0
	}
	return int64(b) * 1000
})

func readProcs(ctx context.Context, statics map[procKey]procStatic) ([]procInfo, error) {
	list, err := gproc.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]procInfo, len(list))
	ok := make([]bool, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, gp := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() == nil {
				out[i], ok[i] = readProc(ctx, gp, statics)
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	kept := out[:0]
	for i, p := range out {
		if ok[i] {
			kept = append(kept, p)
		}
	}
	return kept, nil
}

func readProc(ctx context.Context, gp *gproc.Process, statics map[procKey]procStatic) (procInfo, bool) {
	create, err := gp.CreateTimeWithContext(ctx)
	if err != nil {
		if running, _ := gp.IsRunningWithContext(ctx); !running && gp.Pid > 4 {
			return procInfo{}, false // exited while listing
		}
		create = bootMillis()
	}
	p := procInfo{PID: gp.Pid, Create: create}
	if st, seen := statics[procKey{gp.Pid, create}]; seen {
		p.Name, p.User, p.Command = st.name, st.user, st.command
	} else {
		if n, err := gp.NameWithContext(ctx); err == nil && n != "" {
			p.Name = n
		} else {
			p.Name = "?"
		}
		if u, err := gp.UsernameWithContext(ctx); err == nil {
			p.User = u
		}
		if cmd, err := gp.CmdlineWithContext(ctx); err == nil {
			p.Command = capCommand(cmd)
		}
	}
	if pp, err := gp.PpidWithContext(ctx); err == nil {
		p.PPID = pp
	}
	if mi, err := gp.MemoryInfoWithContext(ctx); err == nil {
		p.RSS = mi.RSS
	}
	if n, err := gp.NumThreadsWithContext(ctx); err == nil {
		p.Threads = n
	}
	if t, err := gp.TimesWithContext(ctx); err == nil {
		p.CPUSecs = t.User + t.System
	}
	if io, err := gp.IOCountersWithContext(ctx); err == nil {
		p.Read, p.Write, p.HaveIO = io.ReadBytes, io.WriteBytes, true
	}
	return p, true
}

func capCommand(s string) string {
	if len(s) <= maxCommandBytes {
		return s
	}
	cut := maxCommandBytes
	for cut > 0 && s[cut]&0xC0 == 0x80 { // do not split a UTF-8 sequence
		cut--
	}
	return s[:cut] + "…"
}

// readListeners maps each PID to the TCP ports it listens on and the UDP
// ports it has bound.
func readListeners(ctx context.Context) (map[int32][]model.Port, error) {
	conns, err := gnet.ConnectionsWithContext(ctx, "inet")
	if err != nil {
		return nil, err
	}
	out := map[int32][]model.Port{}
	seen := map[[3]int64]bool{}
	for _, c := range conns {
		if c.Pid <= 0 || c.Laddr.Port == 0 {
			continue
		}
		var proto string
		switch {
		case c.Type == syscall.SOCK_STREAM && c.Status == "LISTEN":
			proto = "tcp"
		case c.Type == syscall.SOCK_DGRAM && c.Raddr.Port == 0:
			proto = "udp"
		default:
			continue
		}
		k := [3]int64{int64(c.Pid), int64(c.Laddr.Port), int64(c.Type)}
		if seen[k] {
			continue
		}
		seen[k] = true
		out[c.Pid] = append(out[c.Pid], model.Port{Number: uint16(c.Laddr.Port), Proto: proto})
	}
	return out, nil
}

// Lookup reads one process's identity (start time, as Processes reports it,
// and name) without touching a collector's state; ok is false when no
// process has that PID.
func Lookup(ctx context.Context, pid int32) (model.Process, bool, error) {
	gp, err := gproc.NewProcessWithContext(ctx, pid)
	if err != nil {
		if errors.Is(err, gproc.ErrorProcessNotRunning) {
			return model.Process{}, false, nil
		}
		return model.Process{}, false, err
	}
	p, ok := readProc(ctx, gp, nil)
	if !ok {
		return model.Process{}, false, nil
	}
	return model.Process{PID: pid, StartTime: time.UnixMilli(p.Create), Name: p.Name}, true, nil
}
