// Package pages builds the content area of each page; ui.Shell builds the
// rest of the window around it.
package pages

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
)

// card is one metric card: a title, a headline value and its unit, detail
// lines, and an optional sparkline.
type card struct {
	title   string
	value   string // headline number; format.Unknown when not known, "" for none
	unit    string
	details []string
	series  string // hub.History series name; "" for none
}

// Overview builds the Overview page's content for one host.
func Overview(c *ui.Context, v hub.HostView, now time.Time) {
	s := v.Snap
	if s == nil {
		components.EmptyState(c, "Waiting for data…")
		return
	}
	g := float32(components.Grid)
	ui.Column(c).Gap(2 * g).Children(func() {
		ui.Row(c).Gap(2 * g).Wrap().AlignItems(ui.Stretch).Children(func() {
			for _, cd := range cards(s) {
				var series []float64
				if cd.series != "" && v.Hist != nil {
					series = recent(v.Hist.Series(cd.series), now)
				}
				components.Card(c, cd.title, cd.value, cd.unit, cd.details, series)
			}
		})
		ui.Text(c, "Uptime "+format.Duration(s.Uptime)).FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

// recent is the values of the points in the chart window before now, so
// cards and charts show the same span.
func recent(pts []hub.Point, now time.Time) []float64 {
	from := now.Add(-components.ChartWindow)
	var out []float64
	for _, p := range pts {
		if !p.T.Before(from) {
			out = append(out, p.V)
		}
	}
	return out
}

// gpuSeries is the History series of GPU i. History records only the first
// GPU (as "gpu"); the others have no series yet.
func gpuSeries(i int) string {
	if i == 0 {
		return "gpu"
	}
	return fmt.Sprintf("gpu.%d", i)
}

func partial(s *model.Snapshot, section string) bool { return slices.Contains(s.Partial, section) }

func ptr(v float64) *float64 { return &v }

func ratio(used, total uint64) *float64 {
	if total == 0 {
		return nil
	}
	return ptr(float64(used) / float64(total) * 100)
}

// sumRate adds the known rates; nil when none is known.
func sumRate(rates []*float64) *float64 {
	var total float64
	ok := false
	for _, r := range rates {
		if r != nil {
			total += *r
			ok = true
		}
	}
	if !ok {
		return nil
	}
	return &total
}

func cards(s *model.Snapshot) []card {
	var out []card

	cpu := card{title: "CPU", value: format.Unknown, unit: "%", series: "cpu"}
	load := format.Unknown
	if !partial(s, "cpu") {
		cpu.value = format.Number(ptr(s.CPU.Total))
		load = format.Load(s.CPU.Load)
	}
	cpu.details = []string{"Load " + load}
	out = append(out, cpu)

	mem := card{title: "Memory", value: format.Unknown, unit: "%", series: "mem"}
	memDetail := format.Unknown
	if !partial(s, "memory") {
		mem.value = format.Number(ratio(s.Memory.Used, s.Memory.Total))
		if s.Memory.Total > 0 {
			memDetail = format.Bytes(s.Memory.Used) + " / " + format.Bytes(s.Memory.Total)
		}
	}
	mem.details = []string{memDetail}
	out = append(out, mem)

	disk := card{title: "Disk", value: format.Unknown, unit: "%"}
	diskDetail := format.Unknown
	if !partial(s, "disks") && len(s.Disks) > 0 {
		var used, total uint64
		for _, d := range s.Disks {
			used += d.Used
			total += d.Total
		}
		disk.value = format.Number(ratio(used, total))
		if total > 0 {
			diskDetail = format.Bytes(used) + " / " + format.Bytes(total)
		}
	}
	disk.details = []string{diskDetail}
	out = append(out, disk)

	net := card{title: "Network", value: "", series: "net.rx"}
	rx, tx := format.Unknown, format.Unknown
	if !partial(s, "net") {
		var rxs, txs []*float64
		for _, i := range s.Net {
			if i.Name == "lo" || i.Name == "lo0" || !i.Up {
				continue
			}
			rxs, txs = append(rxs, i.RxBps), append(txs, i.TxBps)
		}
		rx, tx = format.Rate(sumRate(rxs)), format.Rate(sumRate(txs))
	}
	net.details = []string{"In " + rx, "Out " + tx}
	out = append(out, net)

	for i, g := range s.GPUs {
		name := g.Name
		if name == "" {
			name = fmt.Sprintf("GPU %d", i+1)
		}
		gpu := card{title: name, value: format.Unknown, unit: "%", series: gpuSeries(i)}
		if !partial(s, "gpu") {
			gpu.value = format.Number(g.UtilPct)
			if g.MemUsed != nil && g.MemTotal != nil {
				gpu.details = append(gpu.details, format.Bytes(*g.MemUsed)+" / "+format.Bytes(*g.MemTotal))
			}
		}
		out = append(out, gpu)
	}

	if b := s.Battery; b != nil {
		bat := card{title: "Battery", value: format.Unknown, unit: "%"}
		if !partial(s, "battery") {
			bat.value = format.Number(ptr(b.ChargePct))
			state := "On battery"
			switch {
			case b.Charging:
				state = "Charging"
			case b.OnAC:
				state = "On AC"
			}
			bat.details = []string{state}
			if b.TimeRemaining != nil {
				bat.details = append(bat.details, format.Duration(*b.TimeRemaining)+" remaining")
			}
		}
		out = append(out, bat)
	}
	return out
}
