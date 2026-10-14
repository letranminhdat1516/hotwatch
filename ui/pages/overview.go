// Package pages builds the content area of each page. E2 will move the card
// and sparkline drawing here onto the shared components.
package pages

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/format"
)

// card is one metric card: a title, a headline value with its unit, detail
// lines, and an optional sparkline.
type card struct {
	title   string
	value   string // headline, with unit; format.Unknown when not known
	details []string
	series  string  // hub.History series name; "" for none
	ceiling float64 // fixed top of the sparkline scale; 0 scales to the data
}

// Overview builds the Overview page for one host.
func Overview(c *ui.Context, v hub.HostView, now time.Time) {
	s := v.Snap
	if s == nil {
		ui.Text(c, "Waiting for data…").TextColor(c.Theme().TextMuted)
		return
	}
	ui.Column(c).Gap(16).Children(func() {
		ui.Row(c).Gap(16).Wrap().AlignItems(ui.Start).Children(func() {
			for _, cd := range cards(s) {
				drawCard(c, cd, v.Hist)
			}
		})
		ui.Text(c, "Uptime "+format.Duration(s.Uptime)).TextColor(c.Theme().TextMuted)
	})
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

	cpu := card{title: "CPU", value: format.Unknown, series: "cpu", ceiling: 100}
	load := format.Unknown
	if !partial(s, "cpu") {
		cpu.value = format.Percent(ptr(s.CPU.Total))
		load = fmt.Sprintf("%.2f / %.2f / %.2f", s.CPU.Load[0], s.CPU.Load[1], s.CPU.Load[2])
	}
	cpu.details = []string{"Load " + load}
	out = append(out, cpu)

	mem := card{title: "Memory", value: format.Unknown, series: "mem", ceiling: 100}
	memDetail := format.Unknown
	if !partial(s, "memory") {
		mem.value = format.Percent(ratio(s.Memory.Used, s.Memory.Total))
		if s.Memory.Total > 0 {
			memDetail = format.Bytes(s.Memory.Used) + " / " + format.Bytes(s.Memory.Total)
		}
	}
	mem.details = []string{memDetail}
	out = append(out, mem)

	disk := card{title: "Disk", value: format.Unknown}
	diskDetail := format.Unknown
	if !partial(s, "disks") && len(s.Disks) > 0 {
		var used, total uint64
		for _, d := range s.Disks {
			used += d.Used
			total += d.Total
		}
		disk.value = format.Percent(ratio(used, total))
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
			rxs, txs = append(rxs, i.RxBps), append(txs, i.TxBps)
		}
		rx, tx = format.Rate(sumRate(rxs)), format.Rate(sumRate(txs))
	}
	net.details = []string{"In " + rx, "Out " + tx}
	out = append(out, net)

	if len(s.GPUs) > 0 {
		g := s.GPUs[0]
		gpu := card{title: "GPU", value: format.Unknown, series: "gpu", ceiling: 100, details: []string{g.Name}}
		if !partial(s, "gpu") {
			gpu.value = format.Percent(g.UtilPct)
			if g.MemUsed != nil && g.MemTotal != nil {
				gpu.details = append(gpu.details, format.Bytes(*g.MemUsed)+" / "+format.Bytes(*g.MemTotal))
			}
		}
		out = append(out, gpu)
	}

	if b := s.Battery; b != nil {
		bat := card{title: "Battery", value: format.Unknown}
		if !partial(s, "battery") {
			bat.value = format.Percent(ptr(b.ChargePct))
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

func drawCard(c *ui.Context, cd card, hist *hub.History) {
	t := c.Theme()
	ui.Column(c).Width(260).Padding(14).Gap(6).Radius(t.Radius).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Text(c, cd.title).TextColor(t.TextMuted)
		if cd.value != "" {
			ui.Text(c, cd.value).FontSize(26).Bold()
		}
		for _, d := range cd.details {
			ui.Text(c, d).TextColor(t.TextMuted)
		}
		if cd.series != "" && hist != nil {
			sparkline(c, hist.Series(cd.series), cd.ceiling)
		}
	})
}

// sparkline draws pts as a thin line; nothing for fewer than two points.
func sparkline(c *ui.Context, pts []hub.Point, ceiling float64) {
	box := ui.Box(c).Height(36)
	if len(pts) < 2 {
		return
	}
	top := ceiling
	if top == 0 {
		for _, p := range pts {
			top = max(top, p.V)
		}
	}
	if top <= 0 {
		top = 1
	}
	xs := make([]float32, len(pts))
	ys := make([]float32, len(pts))
	for i, p := range pts {
		xs[i] = float32(i) / float32(len(pts)-1)
		ys[i] = float32(min(max(p.V/top, 0), 1))
	}
	accent := c.Theme().Accent
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.StrokePath(chart.Line(r, xs, ys, chart.CurveLinear), 1.5, accent)
	})
}
