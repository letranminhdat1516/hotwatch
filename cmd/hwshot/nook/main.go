// Command nook renders the nook's island and idle bar to a PNG over a
// stand-in desktop, for iterating on their look. Dev-only.
//
//	go run ./cmd/hwshot/nook /tmp/nook.png [progress]
package main

import (
	"image/png"
	"os"
	"strconv"
	"time"

	mui "github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/nook"
)

func fp(v float64) *float64 { return &v }

func main() {
	out, progress := "/tmp/nook.png", 1.0
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if len(os.Args) > 2 {
		progress, _ = strconv.ParseFloat(os.Args[2], 64)
	}
	now := time.Now()
	h := hub.NewHistory()
	for i := 60; i >= 0; i-- {
		w := float64(i%9) * 3
		h.Push(now.Add(-time.Duration(i)*time.Second), model.Snapshot{
			CPU: model.CPU{Total: 20 + w}, Memory: model.Memory{Total: 24 << 30, Used: 19 << 30},
			Net:     []model.Interface{{Name: "en0", Up: true, RxBps: fp(20<<10 + w*1024)}},
			Battery: &model.Battery{ChargePct: 98, PowerW: fp(8 + w/5)},
		})
	}
	v := hub.HostView{
		Info: model.HostInfo{OS: "darwin", Caps: model.Caps{Battery: true}},
		Conn: hub.Conn{State: hub.Online}, Hist: h,
		Snap: &model.Snapshot{CPU: model.CPU{Total: 32, PerCore: make([]float64, 10)}, Memory: model.Memory{Total: 24 << 30, Used: 19 << 30},
			Net:     []model.Interface{{Name: "en0", Up: true, RxBps: fp(27 << 10), TxBps: fp(15 << 10)}},
			Battery: &model.Battery{ChargePct: 98, PowerW: fp(10.2)}},
		Procs: []model.Process{{Name: "node", CPUPct: 66.3}, {Name: "WindowServer", CPUPct: 43.8}},
	}
	in := nook.IslandInput{Progress: progress, NotchW: 185, NotchH: 32, Now: now, Selected: "local", View: v,
		Hosts: []nook.Chip{{ID: "local", Name: "This Mac", State: hub.Online}, {ID: "box", Name: "agent-box", State: hub.Online}, {ID: "vps", Name: "VPS", State: hub.Offline}}}
	// Two scenes on a desktop-coloured ground: the idle ears under a menu
	// bar, then the open island, each from the top of the "screen".
	view := func(c *mui.Context) {
		mui.Column(c).Fill().Background(mui.Hex("#b9c7dc")).Gap(24).Children(func() {
			mui.Box(c).Height(70).Children(func() {
				mui.Box(c).Height(32).Background(mui.Hex("#d5dbe6")).Fill()
			}).Draw(nil)
			mui.Row(c).Justify(mui.Center).Margin(-94, 0, 0, 0).Children(func() {
				mui.Box(c).Size(nook.EarsW(185), 32).Children(func() { nook.Ears(c, v, 185) })
			})
			mui.Row(c).Justify(mui.Center).Margin(40, 0, 0, 0).Children(func() {
				mui.Box(c).Size(nook.PanelW, nook.PanelH).Children(func() { nook.Island(c, in) })
			})
		})
	}
	img := mui.Render(view, 760, 380, 2)
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
