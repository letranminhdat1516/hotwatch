package linux

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hotwatch/model"
)

const (
	clkTck     = 100  // USER_HZ; fixed at 100 on every Linux ABI we support
	maxCmdline = 4096 // bytes of command kept per process
)

type procKey struct {
	pid   int32
	start uint64 // starttime ticks; distinguishes a reused PID
}

type procCounters struct {
	ticks  uint64 // utime+stime
	rd, wr uint64 // /proc/<pid>/io read_bytes, write_bytes
	haveIO bool
}

// procState is the baseline for process deltas.
type procState struct {
	mu   sync.Mutex
	prev map[procKey]procCounters
	at   time.Time
}

// scanned is the raw result of reading one /proc/<pid>.
type scanned struct {
	ok   bool
	key  procKey
	cnt  procCounters
	proc model.Process
	uid  string
}

// Processes lists all processes. CPUPct is a delta against the previous call
// for the same (pid, starttime); the first sighting of a process reports 0
// because a process list has no Partial marker. Disk rates are nil on first
// sight and whenever /proc/<pid>/io is unreadable. /proc/<pid>/environ is
// never opened.
func (c *collector) Processes(ctx context.Context) ([]model.Process, error) {
	if err := ctx.Err(); err != nil {
		return []model.Process{}, err
	}
	now := c.now()
	btime, err := readBtime(c.proc("stat"))
	if err != nil {
		return []model.Process{}, err
	}
	users := readPasswd(c.root + "/etc/passwd")
	listeners := readListeners(c.proc(""))

	d, err := os.Open(c.proc(""))
	if err != nil {
		return []model.Process{}, err
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return []model.Process{}, err
	}
	pids := make([]int32, 0, len(names))
	for _, n := range names {
		if v, err := strconv.ParseInt(n, 10, 32); err == nil && v > 0 {
			pids = append(pids, int32(v))
		}
	}

	results := make([]scanned, len(pids))
	workers := min(runtime.GOMAXPROCS(0), 8)
	var wg sync.WaitGroup
	next := make(chan int, 64)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 8192)
			for i := range next {
				results[i] = c.scanPID(pids[i], buf, btime, listeners)
			}
		}()
	}
feed:
	for i := range pids {
		select {
		case next <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return []model.Process{}, err
	}

	c.procs.mu.Lock()
	defer c.procs.mu.Unlock()
	dt := now.Sub(c.procs.at)
	haveBase := c.procs.prev != nil && dt > 0
	cur := make(map[procKey]procCounters, len(results))
	out := make([]model.Process, 0, len(results))
	for i := range results {
		r := &results[i]
		if !r.ok {
			continue // exited between listing and reading
		}
		p := r.proc
		p.User = users.name(r.uid)
		cur[r.key] = r.cnt
		if prev, seen := c.procs.prev[r.key]; haveBase && seen {
			if r.cnt.ticks >= prev.ticks {
				p.CPUPct = float64(r.cnt.ticks-prev.ticks) / dt.Seconds() / clkTck * 100
			}
			if r.cnt.haveIO && prev.haveIO {
				p.DiskReadBps = model.Rate(prev.rd, r.cnt.rd, dt)
				p.DiskWriteBps = model.Rate(prev.wr, r.cnt.wr, dt)
			}
		}
		out = append(out, p)
	}
	c.procs.prev, c.procs.at = cur, now
	slices.SortFunc(out, func(a, b model.Process) int { return int(a.PID) - int(b.PID) })
	return out, nil
}

func (c *collector) scanPID(pid int32, buf []byte, btime int64, listeners map[uint64][]model.Port) (r scanned) {
	dir := c.proc(strconv.Itoa(int(pid)))
	b, err := readInto(dir+"/stat", buf)
	if err != nil {
		return r
	}
	st, ok := parseStat(b)
	if !ok {
		return r
	}
	r.key = procKey{pid: pid, start: st.start}
	r.cnt.ticks = st.utime + st.stime
	r.proc = model.Process{
		PID:       pid,
		PPID:      st.ppid,
		StartTime: startTime(btime, st.start),
		Name:      st.comm,
		Threads:   st.threads,
	}
	if b, err := readInto(dir+"/status", buf); err == nil {
		uid, threads := parseStatus(b)
		r.uid = uid
		if threads > 0 {
			r.proc.Threads = threads
		}
	}
	if b, err := readInto(dir+"/statm", buf); err == nil {
		r.proc.RSS = parseStatmRSS(b) * uint64(c.pagesize)
	}
	if f, err := os.Open(dir + "/cmdline"); err == nil {
		n, _ := io.ReadFull(f, buf[:maxCmdline])
		f.Close()
		r.proc.Command = cmdlineString(buf[:n])
	}
	if b, err := readInto(dir+"/io", buf); err == nil {
		r.cnt.rd, r.cnt.wr, r.cnt.haveIO = parseIO(b)
	}
	r.proc.Ports = processPorts(dir+"/fd", listeners)
	r.ok = true
	return r
}

// startTime converts starttime ticks since boot to wall-clock time. The
// collector and ProcessStart share it so values compare exactly.
func startTime(btime int64, ticks uint64) time.Time {
	return time.Unix(btime+int64(ticks/clkTck), int64(ticks%clkTck)*(1e9/clkTck))
}

// ProcessStart returns the start time of one process, read from
// <root>/proc/<pid>/stat and btime only (no full process scan). ok is false
// when the process does not exist. The result is identical to the StartTime
// the collector reports for the same process.
func ProcessStart(root string, pid int32) (start time.Time, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(root, "proc", strconv.Itoa(int(pid)), "stat"))
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	st, parsed := parseStat(b)
	if !parsed {
		return time.Time{}, false, errors.New("unparseable /proc/<pid>/stat")
	}
	btime, err := readBtime(filepath.Join(root, "proc", "stat"))
	if err != nil {
		return time.Time{}, false, err
	}
	return startTime(btime, st.start), true, nil
}

// readInto reads the whole (small) file into buf, returning the filled part.
func readInto(path string, buf []byte) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

type statFields struct {
	comm                string
	ppid, threads       int32
	utime, stime, start uint64
}

// parseStat parses /proc/<pid>/stat. comm may contain spaces and parens, so
// fields are counted after the last ')'.
func parseStat(b []byte) (statFields, bool) {
	l, r := bytes.IndexByte(b, '('), bytes.LastIndexByte(b, ')')
	if l < 0 || r < l {
		return statFields{}, false
	}
	f := strings.Fields(string(b[r+1:]))
	// f[0] is field 3 (state); field n is f[n-3].
	if len(f) < 20 {
		return statFields{}, false
	}
	ppid, e1 := strconv.ParseInt(f[1], 10, 32)
	ut, e2 := strconv.ParseUint(f[11], 10, 64)
	stm, e3 := strconv.ParseUint(f[12], 10, 64)
	th, e4 := strconv.ParseInt(f[17], 10, 32)
	start, e5 := strconv.ParseUint(f[19], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		return statFields{}, false
	}
	return statFields{comm: string(b[l+1 : r]), ppid: int32(ppid), threads: int32(th),
		utime: ut, stime: stm, start: start}, true
}

// parseStatus extracts the real uid (as text) and thread count.
func parseStatus(b []byte) (uid string, threads int32) {
	for len(b) > 0 {
		var line []byte
		line, b, _ = bytes.Cut(b, []byte{'\n'})
		switch {
		case bytes.HasPrefix(line, []byte("Uid:")):
			if f := bytes.Fields(line[4:]); len(f) > 0 {
				uid = string(f[0])
			}
		case bytes.HasPrefix(line, []byte("Threads:")):
			if v, err := strconv.ParseInt(string(bytes.TrimSpace(line[8:])), 10, 32); err == nil {
				threads = int32(v)
			}
		}
	}
	return uid, threads
}

// parseStatmRSS returns resident pages (field 2).
func parseStatmRSS(b []byte) uint64 {
	f := bytes.Fields(b)
	if len(f) < 2 {
		return 0
	}
	v, _ := strconv.ParseUint(string(f[1]), 10, 64)
	return v
}

func parseIO(b []byte) (rd, wr uint64, ok bool) {
	var gotR, gotW bool
	for len(b) > 0 {
		var line []byte
		line, b, _ = bytes.Cut(b, []byte{'\n'})
		k, v, found := bytes.Cut(line, []byte{':'})
		if !found {
			continue
		}
		n, err := strconv.ParseUint(string(bytes.TrimSpace(v)), 10, 64)
		if err != nil {
			continue
		}
		switch string(k) {
		case "read_bytes":
			rd, gotR = n, true
		case "write_bytes":
			wr, gotW = n, true
		}
	}
	return rd, wr, gotR && gotW
}

// cmdlineString turns NUL-separated argv into a space-joined string capped
// at maxCmdline bytes without splitting a UTF-8 sequence.
func cmdlineString(b []byte) string {
	if len(b) > maxCmdline {
		b = b[:maxCmdline]
		j := len(b) - 1
		for j > 0 && !utf8.RuneStart(b[j]) && len(b)-j < utf8.UTFMax {
			j--
		}
		if !utf8.FullRune(b[j:]) {
			b = b[:j]
		}
	}
	b = bytes.TrimRight(b, "\x00")
	return string(bytes.ReplaceAll(b, []byte{0}, []byte{' '}))
}

func readBtime(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		}
	}
	return 0, errors.New("btime missing from /proc/stat")
}

type passwd map[string]string

// name maps a numeric uid to its user name, falling back to the number.
func (p passwd) name(uid string) string {
	if n, ok := p[uid]; ok {
		return n
	}
	return uid
}

func readPasswd(path string) passwd {
	out := passwd{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 3 && f[0] != "" {
			if _, dup := out[f[2]]; !dup {
				out[f[2]] = f[0]
			}
		}
	}
	return out
}
