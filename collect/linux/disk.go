package linux

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"hotwatch/model"
)

// pseudoFS lists filesystem types that are not real storage. Prefix "cgroup"
// is handled separately. Beyond the required set (tmpfs, overlay, squashfs,
// proc, sysfs, cgroup*, devtmpfs, nsfs) it also drops the other kernel
// pseudo-filesystems that /proc/mounts lists on every host.
var pseudoFS = map[string]bool{
	"tmpfs": true, "overlay": true, "squashfs": true, "proc": true, "sysfs": true,
	"devtmpfs": true, "nsfs": true,
	"devpts": true, "mqueue": true, "pstore": true, "securityfs": true, "debugfs": true,
	"tracefs": true, "bpf": true, "configfs": true, "autofs": true, "hugetlbfs": true,
	"binfmt_misc": true, "fusectl": true, "rpc_pipefs": true, "ramfs": true, "efivarfs": true,
}

func isRealFS(fs string) bool {
	return !pseudoFS[fs] && !strings.HasPrefix(fs, "cgroup")
}

// mountEntry is one real filesystem from /proc/mounts.
type mountEntry struct{ dev, mount, fs string }

// readMounts returns the real filesystems, one entry per mount point.
func readMounts(path string) ([]mountEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []mountEntry
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || !isRealFS(fields[2]) {
			continue
		}
		m := mountEntry{dev: unescapeMount(fields[0]), mount: unescapeMount(fields[1]), fs: fields[2]}
		if seen[m.mount] {
			continue
		}
		seen[m.mount] = true
		out = append(out, m)
	}
	return out, sc.Err()
}

// unescapeMount decodes the \NNN octal escapes /proc/mounts uses for spaces.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// diskCounters are cumulative bytes for one block device.
type diskCounters struct{ read, write uint64 }

// readDiskstats maps device name (sda1) to cumulative bytes (sectors x 512).
func readDiskstats(path string) (map[string]diskCounters, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]diskCounters{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		r, err1 := strconv.ParseUint(fields[5], 10, 64)
		w, err2 := strconv.ParseUint(fields[9], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out[fields[2]] = diskCounters{read: r * 512, write: w * 512}
	}
	return out, sc.Err()
}

// statfsFunc returns capacity of a mount point.
type statfsFunc func(mount string) (total, free uint64, err error)

// sampleDisks fills s.Disks. It returns false when part of the data was
// unreadable. Rates are nil on the first sample, after a counter reset, and
// for devices without a /proc/diskstats entry (for example LVM names).
func (c *collector) sampleDisks(ctx context.Context, now time.Time, out *[]model.Volume) bool {
	mounts, err := readMounts(c.proc("mounts"))
	if err != nil {
		return false
	}
	ok := true
	stats, err := readDiskstats(c.proc("diskstats"))
	if err != nil {
		ok = false
	}
	dt := now.Sub(c.prevDiskAt)
	vols := make([]model.Volume, 0, len(mounts))
	for _, m := range mounts {
		total, free, err := c.boundedStatfs(ctx, m.mount)
		if err != nil {
			ok = false
			continue
		}
		// Device lets bind mounts of one device count their capacity once.
		v := model.Volume{Mount: m.mount, FS: m.fs, Device: m.dev, Total: total, Used: total - min(free, total)}
		dev := filepath.Base(m.dev)
		if cur, found := stats[dev]; found && strings.HasPrefix(m.dev, "/dev/") {
			r, w := cur.read, cur.write
			v.ReadBytes, v.WriteBytes = &r, &w
			if prev, had := c.prevDisk[dev]; had {
				v.ReadBps = model.Rate(prev.read, cur.read, dt)
				v.WriteBps = model.Rate(prev.write, cur.write, dt)
			}
		}
		vols = append(vols, v)
	}
	sort.Slice(vols, func(i, j int) bool { return vols[i].Mount < vols[j].Mount })
	*out = vols
	if stats != nil {
		c.prevDisk, c.prevDiskAt = stats, now
	}
	return ok
}

const defaultStatfsTimeout = 2 * time.Second

var errStatfsBusy = errors.New("statfs still outstanding for mount")

type statfsResult struct {
	total, free uint64
	err         error
}

// boundedStatfs runs statfs in a goroutine so a hung NFS/CIFS/FUSE mount
// cannot stall sampling. On timeout or ctx cancellation it returns an error;
// the mount stays marked busy (and is skipped) until the stuck call returns,
// so goroutines cannot pile up across samples.
func (c *collector) boundedStatfs(ctx context.Context, mount string) (uint64, uint64, error) {
	c.statMu.Lock()
	if c.statBusy[mount] {
		c.statMu.Unlock()
		return 0, 0, errStatfsBusy
	}
	if c.statBusy == nil {
		c.statBusy = map[string]bool{}
	}
	c.statBusy[mount] = true
	c.statMu.Unlock()

	fn := c.statfs
	ch := make(chan statfsResult, 1)
	go func() {
		t, f, err := fn(mount)
		c.statMu.Lock()
		delete(c.statBusy, mount)
		c.statMu.Unlock()
		ch <- statfsResult{t, f, err}
	}()
	timeout := c.statfsTimeout
	if timeout <= 0 {
		timeout = defaultStatfsTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.total, r.free, r.err
	case <-timer.C:
		return 0, 0, context.DeadlineExceeded
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	}
}
