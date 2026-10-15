package hub

import (
	"sync"
	"time"

	"hotwatch/model"
)

// HistoryLen is the number of points kept per series.
const HistoryLen = 600

// Point is one sample of a series.
type Point struct {
	T time.Time
	V float64
}

type ring struct {
	buf   [HistoryLen]Point
	start int
	n     int
}

func (r *ring) push(p Point) {
	if r.n < HistoryLen {
		r.buf[(r.start+r.n)%HistoryLen] = p
		r.n++
		return
	}
	r.buf[r.start] = p
	r.start = (r.start + 1) % HistoryLen
}

func (r *ring) points() []Point {
	out := make([]Point, r.n)
	for i := range out {
		out[i] = r.buf[(r.start+i)%HistoryLen]
	}
	return out
}

// History keeps the last HistoryLen points of each series. It is safe for one
// writer (the poller) and concurrent readers (the UI).
type History struct {
	mu     sync.Mutex
	series map[string]*ring
	last   map[string]Point   // last sample of each integrated rate series
	totals map[string]float64 // amount accumulated per rate series since start
}

func NewHistory() *History {
	return &History{series: map[string]*ring{}, last: map[string]Point{}, totals: map[string]float64{}}
}

// integrate accumulates a rate series into its session total by the
// trapezoid rule between consecutive samples; a sample not after the last
// one is only remembered.
func (h *History) integrate(name string, t time.Time, v float64) {
	if l, ok := h.last[name]; ok && t.After(l.T) {
		h.totals[name] += (l.V + v) / 2 * t.Sub(l.T).Seconds()
	}
	h.last[name] = Point{T: t, V: v}
}

func (h *History) add(name string, t time.Time, v float64) {
	r := h.series[name]
	if r == nil {
		r = &ring{}
		h.series[name] = r
	}
	r.push(Point{T: t, V: v})
}

// sum adds the non-nil values; ok is false when every value is unknown.
func sum[T any](items []T, f func(T) *float64) (total float64, ok bool) {
	for _, it := range items {
		if v := f(it); v != nil {
			total += *v
			ok = true
		}
	}
	return
}

// Push records s at t. Unknown (nil) values are skipped, never stored as 0.
// "mem" is used memory in percent of total; net series are sums over the up,
// non-loopback interfaces, disk series over distinct devices (one per APFS
// container, see model.DistinctVolumes); "gpu" is the first GPU's
// utilisation. The net and disk rate series also feed session totals (Total).
func (h *History) Push(t time.Time, s model.Snapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.add("cpu", t, s.CPU.Total)
	if s.Memory.Total > 0 {
		h.add("mem", t, float64(s.Memory.Used)/float64(s.Memory.Total)*100)
	}
	ifs := make([]model.Interface, 0, len(s.Net))
	for _, i := range s.Net {
		if i.Up && i.Name != "lo" && i.Name != "lo0" {
			ifs = append(ifs, i)
		}
	}
	if v, ok := sum(ifs, func(i model.Interface) *float64 { return i.RxBps }); ok {
		h.add("net.rx", t, v)
		h.integrate("net.rx", t, v)
	}
	if v, ok := sum(ifs, func(i model.Interface) *float64 { return i.TxBps }); ok {
		h.add("net.tx", t, v)
		h.integrate("net.tx", t, v)
	}
	// Volumes of one device report the same store's rate: count it once.
	disks := model.DistinctVolumes(s.Disks)
	if v, ok := sum(disks, func(d model.Volume) *float64 { return d.ReadBps }); ok {
		h.add("disk.read", t, v)
		h.integrate("disk.read", t, v)
	}
	if v, ok := sum(disks, func(d model.Volume) *float64 { return d.WriteBps }); ok {
		h.add("disk.write", t, v)
		h.integrate("disk.write", t, v)
	}
	if len(s.GPUs) > 0 && s.GPUs[0].UtilPct != nil {
		h.add("gpu", t, *s.GPUs[0].UtilPct)
	}
	if s.Battery != nil {
		h.add("battery", t, s.Battery.ChargePct)
	}
}

// Series returns a copy of the named series, oldest first; nil if it has no
// points or the name is unknown.
func (h *History) Series(name string) []Point {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.series[name]
	if r == nil {
		return nil
	}
	return r.points()
}

// Total is the amount accumulated for a rate series (bytes for net.* and
// disk.*) since this history began, integrating the sampled rates; ok is
// false before the second sample. The history outlives reconnects, so this
// is a per-session figure.
func (h *History) Total(name string) (total float64, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.series[name]
	if r == nil || r.n < 2 {
		return 0, false
	}
	return h.totals[name], true
}

// Stats is the average and the peak of the series' points at or after
// from; ok is false when there are none.
func (h *History) Stats(name string, from time.Time) (avg, peak float64, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.series[name]
	if r == nil {
		return 0, 0, false
	}
	n := 0
	for i := 0; i < r.n; i++ {
		p := r.buf[(r.start+i)%HistoryLen]
		if p.T.Before(from) {
			continue
		}
		if n == 0 || p.V > peak {
			peak = p.V
		}
		avg += p.V
		n++
	}
	if n == 0 {
		return 0, 0, false
	}
	return avg / float64(n), peak, true
}
