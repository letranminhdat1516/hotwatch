package portable

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"hotwatch/model"
)

// usageTimeout bounds one volume's size query: a network drive or an
// unresponsive device must not stall the sample.
const usageTimeout = 2 * time.Second

type diskCounters struct{ read, write uint64 }

type diskState struct {
	prev   map[string]diskCounters
	prevAt time.Time
	mu     sync.Mutex
	busy   map[string]bool // mounts with a size query still running
}

// sampleDisks lists the mounted volumes with their size and, where the
// system reports them, transfer rates. A volume whose size cannot be read
// (an empty card reader or optical drive) is left out.
func (c *collector) sampleDisks(ctx context.Context, now time.Time, out *[]model.Volume) bool {
	parts, err := c.api.partitions(ctx)
	if err != nil {
		return false
	}
	io, _ := c.api.diskIO(ctx)
	st := &c.disks
	dt := now.Sub(st.prevAt)
	next := map[string]diskCounters{}
	vols := make([]model.Volume, 0, len(parts))
	for _, p := range parts {
		u, ok := c.usage(ctx, p.Mountpoint)
		if !ok || u.Total == 0 {
			continue
		}
		v := model.Volume{Mount: p.Mountpoint, FS: p.Fstype, Device: p.Device, Total: u.Total, Used: u.Used}
		if key, ok := ioKey(io, p.Device); ok {
			cur := diskCounters{io[key].ReadBytes, io[key].WriteBytes}
			r, w := cur.read, cur.write
			v.ReadBytes, v.WriteBytes = &r, &w
			if prev, had := st.prev[key]; had {
				v.ReadBps = model.Rate(prev.read, cur.read, dt)
				v.WriteBps = model.Rate(prev.write, cur.write, dt)
			}
			next[key] = cur
		}
		vols = append(vols, v)
	}
	st.prev, st.prevAt = next, now
	*out = vols
	return true
}

// usage reads one volume's size within usageTimeout; a mount whose earlier
// query has not returned yet is skipped instead of piling up goroutines.
func (c *collector) usage(ctx context.Context, mount string) (*disk.UsageStat, bool) {
	st := &c.disks
	st.mu.Lock()
	if st.busy[mount] {
		st.mu.Unlock()
		return nil, false
	}
	if st.busy == nil {
		st.busy = map[string]bool{}
	}
	st.busy[mount] = true
	st.mu.Unlock()
	type result struct {
		u   *disk.UsageStat
		err error
	}
	done := make(chan result, 1)
	go func() {
		u, err := c.api.usage(context.WithoutCancel(ctx), mount)
		st.mu.Lock()
		delete(st.busy, mount)
		st.mu.Unlock()
		done <- result{u, err}
	}()
	select {
	case r := <-done:
		return r.u, r.err == nil && r.u != nil
	case <-time.After(usageTimeout):
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

// ioKey finds a partition's I/O counters: by device ("C:" on Windows), by
// its base name ("ada0p2" for /dev/ada0p2), or by the disk that holds it.
func ioKey(io map[string]disk.IOCountersStat, device string) (string, bool) {
	if device == "" || len(io) == 0 {
		return "", false
	}
	if _, ok := io[device]; ok {
		return device, true
	}
	base := filepath.Base(strings.ReplaceAll(device, `\`, "/"))
	if _, ok := io[base]; ok {
		return base, true
	}
	best := ""
	for k := range io {
		if strings.HasPrefix(base, k) && len(k) > len(best) {
			best = k
		}
	}
	return best, best != ""
}
