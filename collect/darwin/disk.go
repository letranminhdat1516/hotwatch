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

// sampleDisks lists local volumes with usage and per-disk I/O rates. Network
// mounts are skipped (their statfs can block). It returns false when the
// partition list is unreadable.
func (c *collector) sampleDisks(ctx context.Context, now time.Time, out *[]model.Volume) bool {
	parts, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return false
	}
	io, ioErr := disk.IOCountersWithContext(ctx)
	cur := map[string]diskCounters{}
	for n, s := range io {
		cur[n] = diskCounters{read: s.ReadBytes, write: s.WriteBytes}
	}
	dt := now.Sub(c.prevDiskAt)
	seen := map[string]bool{}
	var vols []model.Volume
	for _, p := range parts {
		if !keepVolume(p.Mountpoint, p.Fstype) || !strings.HasPrefix(p.Device, "/dev/") || seen[p.Mountpoint] {
			continue
		}
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue
		}
		seen[p.Mountpoint] = true
		v := model.Volume{Mount: p.Mountpoint, FS: p.Fstype, Total: u.Total, Used: u.Used}
		if name := bsdName(p.Device); name != "" {
			if prev, ok := c.prevDisk[name]; ok {
				if now, ok := cur[name]; ok {
					v.ReadBps = model.Rate(prev.read, now.read, dt)
					v.WriteBps = model.Rate(prev.write, now.write, dt)
				}
			}
		}
		vols = append(vols, v)
	}
	sort.Slice(vols, func(i, j int) bool { return vols[i].Mount < vols[j].Mount })
	*out = vols
	if ioErr == nil {
		c.prevDisk, c.prevDiskAt = cur, now
	} else {
		c.prevDisk = nil
	}
	return len(vols) > 0
}
