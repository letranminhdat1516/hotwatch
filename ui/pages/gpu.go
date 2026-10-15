package pages

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
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

// GPU builds the GPU page: one section per GPU with its utilisation, video
// memory and temperature, and the last five minutes of the first GPU's use.
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
		for _, gpu := range s.GPUs {
			if unknown {
				gpu = model.GPU{Name: gpu.Name}
			}
			section(c, gpu.Name, func() { gpuBody(c, gpu) })
		}
		if unknown {
			return // the newest history point is a carried-over value
		}
		components.DotTimeChart(c, "GPU utilisation", "%", []components.Series{{Name: "GPU", Pts: points(v, "gpu")}}, now, metricTint(c, "gpu"))
		if len(s.GPUs) > 1 {
			note(c, fmt.Sprintf("Chart shows the first GPU (%s)", s.GPUs[0].Name))
		}
	})
}

func gpuBody(c *ui.Context, gpu model.GPU) {
	tint := metricTint(c, "gpu")
	components.BarRow(c, "Usage", gpu.UtilPct, format.Percent(gpu.UtilPct), tint)
	if gpu.MemUsed != nil && gpu.MemTotal != nil && *gpu.MemTotal > 0 {
		components.BarRow(c, "VRAM", ratio(*gpu.MemUsed, *gpu.MemTotal), format.Percent(ratio(*gpu.MemUsed, *gpu.MemTotal)), tint)
		note(c, vram(gpu.MemUsed, gpu.MemTotal))
	} else {
		kv(c, "VRAM", vram(gpu.MemUsed, gpu.MemTotal))
	}
	kv(c, "Temperature", celsius(gpu.TempC))
}
