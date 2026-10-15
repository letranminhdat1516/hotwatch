//go:build darwin

package darwin

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"hotwatch/model"
)

type diskCounters struct{ read, write uint64 }

var bsdRe = regexp.MustCompile(`^(?:/dev/)?(disk\d+)(?:s\d+)*$`)

// bsdName maps a device node like /dev/disk3s1s1 to its whole-disk BSD name
// (disk3); "" when the device is not a local disk.
func bsdName(device string) string {
	if m := bsdRe.FindStringSubmatch(device); m != nil {
		return m[1]
	}
	return ""
}

// keepVolume reports whether a mount is user-visible: it drops devfs, autofs,
// nullfs snapshot mounts and the hidden volumes under /System/Volumes other
// than Data.
func keepVolume(mount, fs string) bool {
	switch fs {
	case "devfs", "autofs", "nullfs":
		return false
	}
	if strings.HasPrefix(mount, "/System/Volumes/") &&
		mount != "/System/Volumes/Data" {
		return false
	}
	return true
}

// physical returns the whole-disk names whose counters describe the volume
// on device. APFS volumes map to their container's physical stores; other
// devices are their own disk. nil means unknown.
func (c *collector) physical(ctx context.Context, name string, io map[string]diskCounters) []string {
	if _, ok := io[name]; ok {
		return []string{name}
	}
	if stores, ok := c.apfs[name]; ok {
		return stores
	}
	if !c.apfsTried[name] { // refresh once per unknown container
		if c.apfsTried == nil {
			c.apfsTried = map[string]bool{}
		}
		c.apfsTried[name] = true
		if data, err := c.apfsList(ctx); err == nil {
			if m, err := parseAPFSList(data); err == nil {
				c.apfs = m
			}
		}
		return c.apfs[name]
	}
	return nil
}

// sumIO adds the counters of names; ok is false if any is missing.
func sumIO(m map[string]diskCounters, names []string) (diskCounters, bool) {
	var t diskCounters
	for _, n := range names {
		v, ok := m[n]
		if !ok {
			return diskCounters{}, false
		}
		t.read += v.read
		t.write += v.write
	}
	return t, len(names) > 0
}

// sampleDisks lists local volumes with usage and I/O rates. Network mounts
// are skipped (their statfs can block). Volumes of one APFS container share
// the rate of the container's physical store(s). It returns false when the
// partition list is unreadable or a local volume's usage could not be read.
func (c *collector) sampleDisks(ctx context.Context, at time.Time, out *[]model.Volume) bool {
	parts, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return false
	}
	io, ioErr := disk.IOCountersWithContext(ctx)
	cur := map[string]diskCounters{}
	for n, s := range io {
		cur[n] = diskCounters{read: s.ReadBytes, write: s.WriteBytes}
	}
	dt := at.Sub(c.prevDiskAt)
	seen := map[string]bool{}
	var vols []model.Volume
	complete := true
	for _, p := range parts {
		if !keepVolume(p.Mountpoint, p.Fstype) || !strings.HasPrefix(p.Device, "/dev/") || seen[p.Mountpoint] {
			continue
		}
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			complete = false
			continue
		}
		seen[p.Mountpoint] = true
		v := model.Volume{Mount: p.Mountpoint, FS: p.Fstype, Device: p.Device, Total: u.Total, Used: u.Used}
		if strings.EqualFold(p.Fstype, "apfs") {
			if name := bsdName(p.Device); name != "" {
				v.Device = name // volumes of one container share its capacity
			}
		}
		if name := bsdName(p.Device); name != "" && c.prevDisk != nil {
			if stores := c.physical(ctx, name, cur); stores != nil {
				p, ok1 := sumIO(c.prevDisk, stores)
				n, ok2 := sumIO(cur, stores)
				if ok1 && ok2 {
					v.ReadBps = model.Rate(p.read, n.read, dt)
					v.WriteBps = model.Rate(p.write, n.write, dt)
				}
			}
		}
		vols = append(vols, v)
	}
	sort.Slice(vols, func(i, j int) bool { return vols[i].Mount < vols[j].Mount })
	*out = vols
	if ioErr == nil {
		c.prevDisk, c.prevDiskAt = cur, at
	} else {
		c.prevDisk = nil
	}
	return len(vols) > 0 && complete
}
