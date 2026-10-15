package nook

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/components"
	"hotwatch/ui/format"
	"hotwatch/ui/icons"
)

// Sizes, DIPs. The idle nook widens the notch by EarW on each side; the
// island is PanelW wide (Shoulder on each side of it is the flare of its
// top corners) and PanelH tall.
const (
	EarW     = 64
	Shoulder = 10
	PanelW   = 560 + 2*Shoulder
	PanelH   = 212
)

// EarsW is the idle window's width for a notch notchW wide.
func EarsW(notchW float32) float32 { return notchW + 2*(EarW+Shoulder) }

// notchShape is the outline of the notch and everything that grows out of
// it: a black body x…x+w wide and h tall from the top of the screen, its
// top corners flared outward by s (so it melts into the bezel like the
// notch itself) and its bottom corners rounded by b.
func notchShape(x, y, w, h, s, b float32) *ui.Path {
	b = min(b, h/2, w/2)
	p := &ui.Path{}
	p.MoveTo(x-s, y)
	p.QuadTo(x, y, x, y+s)
	p.LineTo(x, y+h-b)
	p.QuadTo(x, y+h, x+b, y+h)
	p.LineTo(x+w-b, y+h)
	p.QuadTo(x+w, y+h, x+w, y+h-b)
	p.LineTo(x+w, y+s)
	p.QuadTo(x+w, y, x+w+s, y)
	p.Close()
	return p
}

// The island is always black, like the notch it grows out of, in light and
// dark mode alike; its text and tints are fixed for that ground.
var (
	ink       = ui.Hex("#f3eef0")
	inkMuted  = ui.Hex("#a39aa0")
	chipBg    = ui.Hex("#1f1b1e")
	rose      = ui.Hex("#fb7185")
	roseInk   = ui.Hex("#1c0a10")
	amber     = ui.Hex("#fbbf24")
	offlineFg = ui.Hex("#f87171")
)

// Tile colours on black: the metric hues, lightened.
var tileHex = map[string]string{
	"cpu": "#409cff", "memory": "#30d158", "network": "#40c8e0", "energy": "#ffd60a",
	"gpu": "#ff375f", "docker": "#64d2ff", "disk": "#ff9f0a",
}

type tone int

const (
	toneNormal tone = iota
	toneWarn
	toneOffline
)

// barLevels are the idle bar's CPU and memory fractions (0–1) and its tone:
// offline when the host is not online, warning at 85 % or more of either.
func barLevels(v hub.HostView) (cpu, mem float64, t tone) {
	if v.Conn.State != "" && v.Conn.State != hub.Online {
		t = toneOffline
	}
	if v.Snap == nil {
		return 0, 0, t
	}
	cpu = clamp(v.Snap.CPU.Total / 100)
	if v.Snap.Memory.Total > 0 {
		mem = clamp(float64(v.Snap.Memory.Used) / float64(v.Snap.Memory.Total))
	}
	if t == toneNormal && (cpu >= 0.85 || mem >= 0.85) {
		t = toneWarn
	}
	return cpu, mem, t
}

// Ears is the idle nook: the notch drawn wider, in the same black, with
// the CPU on the left ear and the memory on the right, each as a small
// level meter and its percentage. Amber when either is high; a rose dot
// and "Offline" when the host is not reachable.
func Ears(c *ui.Context, v hub.HostView, notchW float32) {
	cpu, mem, t := barLevels(v)
	cpuCol, memCol := ui.Hex(tileHex["cpu"]), ui.Hex(tileHex["memory"])
	if t == toneWarn {
		if cpu >= 0.85 {
			cpuCol = amber
		}
		if mem >= 0.85 {
			memCol = amber
		}
	}
	root := ui.Row(c).Fill().Background(ui.Transparent).AlignItems(ui.Center).Padding(0, Shoulder+8, 0, Shoulder+8)
	root.Draw(func(p *ui.Painter, r ui.Rect) {
		p.FillPath(notchShape(r.X+Shoulder, r.Y, r.W-2*Shoulder, r.H, Shoulder, 10), ui.RGB(0, 0, 0))
	})
	root.Children(func() {
		ear := func(frac float64, col ui.Color, end bool) {
			row := ui.Row(c).Width(EarW - 10).Gap(6).AlignItems(ui.Center)
			if end {
				row.Justify(ui.End)
			}
			row.Children(func() {
				if t == toneOffline {
					if !end {
						ui.Box(c).Size(6, 6).Radius(3).Background(offlineFg)
						ui.Text(c, "Offline").FontSize(10).FontWeight(600).TextColor(offlineFg).SingleLine()
					}
					return
				}
				meter := func() { levelDots(c, frac, col) }
				label := func() {
					ui.Text(c, fmt.Sprintf("%.0f%%", frac*100)).FontSize(11).FontWeight(650).TextColor(col).NoWrap().Shrink(0)
				}
				if end {
					label()
					meter()
				} else {
					meter()
					label()
				}
			})
		}
		ear(cpu, cpuCol, false)
		ui.Spacer(c) // the notch
		ear(mem, memCol, true)
	})
	_ = notchW
}

// levelDots is a five-step meter of small rounded bars, lit to frac.
func levelDots(c *ui.Context, frac float64, col ui.Color) {
	lit := int(frac*5 + 0.5)
	if frac > 0 && lit == 0 {
		lit = 1
	}
	ui.Box(c).Size(5*3+4*2, 12).Draw(func(p *ui.Painter, r ui.Rect) {
		for i := 0; i < 5; i++ {
			h := float32(4 + 2*i)
			cl := col.Alpha(0.25)
			if i < lit {
				cl = col
			}
			p.Fill(ui.Rect{X: r.X + float32(i)*5, Y: r.Y + r.H - h, W: 3, H: h}, cl, 1.5)
		}
	})
}

// Chip is a host the island offers to switch to.
type Chip struct {
	ID, Name string
	State    hub.ConnState
}

// IslandInput is what the island shows and what it can do.
type IslandInput struct {
	Progress       float64 // 0 closed … 1 open (Motion.Progress)
	NotchW, NotchH float32
	Hosts          []Chip
	Selected       string
	View           hub.HostView // the selected host's
	Now            time.Time
	Paused         bool
	OnHost         func(id string)
	OnPage         func(page string)
	OnOpen         func()
	OnPause        func()
}

// Island draws the black panel growing from the notch's size to the full
// window as Progress goes to 1, and its content once it is nearly open.
func Island(c *ui.Context, in IslandInput) {
	p := float32(clamp(in.Progress))
	// It grows from the idle ears to the full panel.
	startW := in.NotchW + 2*EarW
	body := float32(PanelW - 2*Shoulder)
	w := startW + (body-startW)*p
	h := in.NotchH + (PanelH-in.NotchH)*p
	radius := 10 + 16*p
	root := ui.Column(c).Fill().Background(ui.Transparent)
	root.Draw(func(pt *ui.Painter, r ui.Rect) {
		x := r.X + (r.W-w)/2
		pt.FillPath(notchShape(x, r.Y, w, h, Shoulder, radius), ui.RGB(0, 0, 0))
	})
	if in.Progress < 0.85 {
		return
	}
	root.Padding(0, 18+Shoulder, 12, 18+Shoulder).Gap(8).TextColor(ink).Children(func() {
		header(c, in)
		chips(c, in)
		tiles(c, in)
		topProcs(c, in)
	})
}

func header(c *ui.Context, in IslandInput) {
	ui.Row(c).Height(in.NotchH).AlignItems(ui.Center).Gap(8).Children(func() {
		components.Glyph(c, icons.Energy, 14, rose)
		ui.Text(c, "Hotwatch").FontSize(12).FontWeight(600).TextColor(ink).SingleLine()
		ui.Spacer(c) // the notch sits in the middle of this row
		icon, tip := icons.Pause, "Pause updates"
		if in.Paused {
			icon, tip = icons.Play, "Resume updates"
		}
		if b := iconButton(c, "nook-pause", icon, tip); b && in.OnPause != nil {
			in.OnPause()
		}
		if b := iconButton(c, "nook-open", icons.ChevronRight, "Open Hotwatch"); b && in.OnOpen != nil {
			in.OnOpen()
		}
	})
}

func iconButton(c *ui.Context, key string, icon *ui.SVG, label string) bool {
	b := ui.Row(c).Key(key).Size(24, 24).Radius(7).Center().Background(chipBg).Label(label).Tooltip(label)
	b.Children(func() { components.Glyph(c, icon, 12, inkMuted) })
	return b.Clicked()
}

func chips(c *ui.Context, in IslandInput) {
	ui.Row(c).Gap(6).Children(func() {
		for _, h := range in.Hosts {
			sel := h.ID == in.Selected
			bg, fg, dot := chipBg, ui.Hex("#d8d0d4"), ui.Hex("#34d399")
			if h.State != hub.Online {
				dot = offlineFg
			}
			if sel {
				bg, fg, dot = rose, roseInk, roseInk
			}
			chip := ui.Row(c).Key("chip-"+h.ID).Gap(6).AlignItems(ui.Center).Padding(3, 10).Radius(999).Background(bg)
			if chip.Clicked() && in.OnHost != nil && !sel {
				in.OnHost(h.ID)
			}
			chip.Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(dot)
				ui.Text(c, h.Name).FontSize(11).FontWeight(600).TextColor(fg).SingleLine()
			})
		}
	})
}

// tile is one figure of the island.
type tile struct {
	key, label, side, value, unit, series string
	ceiling                               float64
}

// tilesFor picks four figures for the host: CPU, Memory, Network, then
// Energy on a Mac with a battery, else the GPU, else Docker, else Disk.
func tilesFor(v hub.HostView) []tile {
	s := v.Snap
	unknown := format.Unknown
	cpu := tile{key: "cpu", label: "CPU", value: unknown, unit: "%", series: "cpu", ceiling: 100}
	mem := tile{key: "memory", label: "Memory", value: unknown, series: "mem", ceiling: 100}
	net := tile{key: "network", label: "Network", value: unknown, series: "net.rx"}
	var fourth tile
	if s != nil {
		cpu.value = fmt.Sprintf("%.0f", s.CPU.Total)
		if n := len(s.CPU.PerCore); n > 0 {
			cpu.side = fmt.Sprintf("%d cores", n)
		}
		if s.Memory.Total > 0 {
			mem.value, mem.unit = split(format.Bytes(s.Memory.Used))
		}
		var rx, tx []*float64
		for _, i := range s.Net {
			if i.Up && i.Name != "lo" && i.Name != "lo0" {
				rx, tx = append(rx, i.RxBps), append(tx, i.TxBps)
			}
		}
		net.value, net.unit = split(format.Rate(sum(rx)))
		switch {
		case s.Battery != nil:
			fourth = tile{key: "energy", label: "Energy", value: unknown, unit: "W", series: "power"}
			if s.Battery.PowerW != nil {
				fourth.value = fmt.Sprintf("%.1f", *s.Battery.PowerW)
			}
			fourth.side = fmt.Sprintf("%.0f%%", s.Battery.ChargePct)
		case len(s.GPUs) > 0:
			fourth = tile{key: "gpu", label: "GPU", value: unknown, unit: "%", series: "gpu", ceiling: 100}
			if u := s.GPUs[0].UtilPct; u != nil {
				fourth.value = fmt.Sprintf("%.0f", *u)
			}
		case v.Info.Caps.Docker:
			running := 0
			for _, ct := range v.Ctrs {
				if ct.State == "running" {
					running++
				}
			}
			fourth = tile{key: "docker", label: "Docker", value: fmt.Sprint(running), unit: "running"}
		default:
			var used, total uint64
			for _, d := range model.DistinctVolumes(s.Disks) {
				used, total = used+d.Used, total+d.Total
			}
			fourth = tile{key: "disk", label: "Disk", value: unknown, series: "disk.write"}
			if total > 0 {
				fourth.value, fourth.unit = split(format.Bytes(total - min(used, total)))
				fourth.side = "free"
			}
		}
	} else {
		fourth = tile{key: "disk", label: "Disk", value: unknown}
	}
	return []tile{cpu, mem, net, fourth}
}

func tiles(c *ui.Context, in IslandInput) {
	from := in.Now.Add(-time.Minute)
	ui.Grid(c).Columns(4).Gap(8).Children(func() {
		for _, t := range tilesFor(in.View) {
			col := ui.Hex(tileHex[t.key])
			box := ui.Column(c).Key("tile-"+t.key).Gap(4).Padding(10, 12).Radius(12).Background(col.Alpha(0.14)).Label(t.label)
			if box.Clicked() && in.OnPage != nil {
				in.OnPage(t.key)
			}
			box.Children(func() {
				ui.Row(c).Children(func() {
					ui.Text(c, t.label).FontSize(11).TextColor(inkMuted).SingleLine().Grow(1)
					if t.side != "" {
						ui.Text(c, t.side).FontSize(11).TextColor(inkMuted).SingleLine()
					}
				})
				spans := []ui.Span{{Text: t.value, Size: 22, Weight: 700, Color: ink}}
				if t.unit != "" && t.value != format.Unknown {
					spans = append(spans, ui.Span{Text: " " + t.unit, Size: 12, Weight: 500, Color: inkMuted})
				}
				ui.RichText(c, spans...).SingleLine()
				var pts []hub.Point
				if t.series != "" && in.View.Hist != nil {
					pts = in.View.Hist.Series(t.series)
				}
				components.MiniDots(c, pts, from, in.Now, t.ceiling, col, 28)
			})
		}
	})
}

// topProcs is a strip with the two busiest processes.
func topProcs(c *ui.Context, in IslandInput) {
	ps := slices.Clone(in.View.Procs)
	slices.SortFunc(ps, func(a, b model.Process) int { return cmp.Compare(b.CPUPct, a.CPUPct) })
	if len(ps) > 2 {
		ps = ps[:2]
	}
	if len(ps) == 0 {
		return
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Top").FontSize(12).TextColor(inkMuted)
		for i, p := range ps {
			if i > 0 {
				ui.Box(c).Size(1, 12).Background(ui.Hex("#2a2429"))
			}
			ui.Text(c, p.Name).FontSize(12).TextColor(ink).SingleLine()
			col := ink
			if p.CPUPct >= 50 {
				col = amber
			}
			ui.Text(c, fmt.Sprintf("%.0f%%", p.CPUPct)).FontSize(12).FontWeight(600).TextColor(col).SingleLine()
		}
	})
}

func split(s string) (num, unit string) {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == ' ' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

func sum(rs []*float64) *float64 {
	var t float64
	ok := false
	for _, r := range rs {
		if r != nil {
			t += *r
			ok = true
		}
	}
	if !ok {
		return nil
	}
	return &t
}
