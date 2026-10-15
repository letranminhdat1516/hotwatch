package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// kv is a muted label with its value beside it.
func kv(c *ui.Context, label, value string) {
	ui.Row(c).Gap(g()).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted).Width(18 * g()).SingleLine()
		ui.Text(c, value).FontSize(13).FontWeight(600).SingleLine()
	})
}

// celsius formats a temperature, "—" when unknown.
func celsius(t *float64) string {
	if n := format.Number(t); n != format.Unknown {
		return n + " °C"
	}
	return format.Unknown
}

// vram is the video memory as text: "1.6 / 10.0 GB" when both are known,
// the used amount alone when only it is, "—" otherwise.
func vram(used, total *uint64) string {
	switch {
	case used == nil:
		return format.Unknown
	case total == nil || *total == 0:
		return format.Bytes(*used)
	}
	u, t := format.Bytes(*used), format.Bytes(*total)
	if len(u) > 3 && len(t) > 3 && u[len(u)-3:] == t[len(t)-3:] {
		u = u[:len(u)-3]
	}
	return u + " / " + t
}

// GPU builds the GPU page (the owner's reference): each GPU's load with
// its engines, memory and peak over a dot chart, then what it is (About)
// and how busy each engine is (Engines).
func GPU(c *ui.Context, v hub.HostView, now time.Time) {
	if waiting(c, v) {
		return
	}
	s := v.Snap
	unknown := partial(s, "gpu")
	pageColumn(c, func() {
		if len(s.GPUs) == 0 {
			if unknown {
				section(c, "GPU", func() { note(c, format.Unknown) })
				return
			}
			components.EmptyState(c, "No supported GPU on this host")
			return
		}
		for i, gpu := range s.GPUs {
			if unknown {
				gpu = model.GPU{Name: gpu.Name}
			}
			graphicsCard(c, v, i, gpu, now)
			ui.Grid(c).Columns(2).Gap(2 * g()).Children(func() {
				gpuAboutCard(c, v, gpu)
				enginesCard(c, gpu)
			})
		}
	})
}

// graphicsCard is GPU i's load, its engines, memory and peak, and the dot
// chart of its load (History keeps the first GPU's).
func graphicsCard(c *ui.Context, v hub.HostView, i int, gpu model.GPU, now time.Time) {
	tint := metricTint(c, "gpu")
	from := now.Add(-components.ChartWindow())
	pageCard(c, icons.GPU, "Graphics", "gpu", nil, func() {
		val := format.Unknown
		if gpu.UtilPct != nil {
			val = whole(*gpu.UtilPct)
		}
		var figs []fig
		if gpu.RendererPct != nil {
			figs = append(figs, fig{"Renderer", pctShort(*gpu.RendererPct), ""})
		}
		if gpu.TilerPct != nil {
			figs = append(figs, fig{"Tiler", pctShort(*gpu.TilerPct), ""})
		}
		figs = append(figs, fig{"Memory", vram(gpu.MemUsed, gpu.MemTotal), ""})
		if gpu.TempC != nil {
			figs = append(figs, fig{"Temperature", degrees(*gpu.TempC), ""})
		}
		peak := format.Unknown
		if v.Hist != nil {
			if _, p, ok := v.Hist.Stats(gpuSeries(i), from); ok {
				peak = pctShort(p)
			}
		}
		figs = append(figs, fig{"Peak", peak, ""})
		headline(c, gpu.Name, val, "%", figs)
		components.DotChart(c, points(v, gpuSeries(i)), from, now, 100, tint, 200).Margin(g(), 0, 0, 0)
	})
}

// gpuAboutCard says what the GPU is and how much memory it holds.
func gpuAboutCard(c *ui.Context, v hub.HostView, gpu model.GPU) {
	pageCard(c, icons.Info, "About", "gpu", nil, func() {
		kvRow(c, "Model", gpu.Name)
		if n := v.Info.Machine.GPUCores; n > 0 {
			kvRow(c, "Cores", fmt.Sprint(n))
		}
		if gpu.MemUsed != nil {
			kvRow(c, "Memory in use", format.Bytes(*gpu.MemUsed))
		}
		if gpu.MemAlloc != nil {
			kvRow(c, "Memory allocated", format.Bytes(*gpu.MemAlloc))
		}
		switch {
		case gpu.MemTotal != nil && *gpu.MemTotal > 0:
			kvRow(c, "Video memory", format.Bytes(*gpu.MemTotal))
		case v.Info.OS == "darwin" && v.Snap.Memory.Total > 0:
			kvRow(c, "Unified memory", format.Bytes(v.Snap.Memory.Total))
		}
		if gpu.TempC != nil {
			kvRow(c, "Temperature", degrees(*gpu.TempC))
		}
	})
}

// enginesCard is a bar per engine the driver reports.
func enginesCard(c *ui.Context, gpu model.GPU) {
	tint := metricTint(c, "gpu")
	pageCard(c, icons.Activity, "Engines", "gpu", nil, func() {
		bar := func(label string, p *float64) {
			if p == nil {
				return
			}
			labelBar(c, label, "", *p/100, tint, pctShort(*p))
		}
		bar("Device", gpu.UtilPct)
		bar("Renderer", gpu.RendererPct)
		bar("Tiler", gpu.TilerPct)
		if gpu.MemUsed != nil && gpu.MemTotal != nil && *gpu.MemTotal > 0 {
			r := float64(*gpu.MemUsed) / float64(*gpu.MemTotal) * 100
			bar("Memory", &r)
		}
	})
}
