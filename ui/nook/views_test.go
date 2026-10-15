package nook

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
)

func fp(v float64) *float64 { return &v }

func macView() hub.HostView {
	return hub.HostView{
		Info: model.HostInfo{OS: "darwin", Caps: model.Caps{Battery: true, GPU: true, Processes: true}},
		Conn: hub.Conn{State: hub.Online},
		Snap: &model.Snapshot{
			CPU:     model.CPU{Total: 32, PerCore: make([]float64, 10)},
			Memory:  model.Memory{Total: 24 << 30, Used: 19 << 30},
			Net:     []model.Interface{{Name: "en0", Up: true, RxBps: fp(27 << 10), TxBps: fp(15 << 10)}},
			Battery: &model.Battery{ChargePct: 98, PowerW: fp(10.2)},
		},
		Procs: []model.Process{{PID: 2, Name: "node", CPUPct: 66.3}, {PID: 6, Name: "WindowServer", CPUPct: 43.8}, {PID: 7, Name: "launchd", CPUPct: 0.2}},
	}
}

func boxView() hub.HostView {
	return hub.HostView{
		Info: model.HostInfo{OS: "linux", Caps: model.Caps{GPU: true, Docker: true, Processes: true}},
		Conn: hub.Conn{State: hub.Online},
		Snap: &model.Snapshot{
			CPU:    model.CPU{Total: 71, PerCore: make([]float64, 16)},
			Memory: model.Memory{Total: 62 << 30, Used: 41 << 30},
			GPUs:   []model.GPU{{Name: "RTX 3080", UtilPct: fp(63)}},
		},
	}
}

type recorder struct {
	hosts, pages []string
	opened       int
}

func islandTester(v hub.HostView, progress float64, r *recorder) *ui.Tester {
	in := IslandInput{
		Progress: progress, NotchW: 185, NotchH: 32, Now: time.Now(),
		Hosts:    []Chip{{ID: "local", Name: "This Mac", State: hub.Online}, {ID: "box", Name: "agent-box", State: hub.Online}, {ID: "vps", Name: "VPS", State: hub.Offline}},
		Selected: "local", View: v,
		OnHost: func(id string) { r.hosts = append(r.hosts, id) },
		OnPage: func(p string) { r.pages = append(r.pages, p) },
		OnOpen: func() { r.opened++ },
	}
	return ui.NewTester(func(c *ui.Context) { Island(c, in) }, int(PanelW), int(PanelH))
}

func TestIslandShowsTheSelectedHostAndActs(t *testing.T) {
	r := &recorder{}
	tt := islandTester(macView(), 1, r)
	for _, want := range []string{"This Mac", "agent-box", "VPS", "CPU", "32 %", "Memory", "19.0 GB", "Network", "27.0 KB/s", "Energy", "10.2 W", "node", "66%"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if err := tt.Click("agent-box"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Memory"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Open Hotwatch"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.hosts, []string{"box"}) || !slices.Equal(r.pages, []string{"memory"}) || r.opened != 1 {
		t.Fatalf("hosts %v pages %v opened %d", r.hosts, r.pages, r.opened)
	}
}

func TestIslandOnALinuxHostShowsItsGPU(t *testing.T) {
	tt := islandTester(boxView(), 1, &recorder{})
	if !slices.Contains(tt.Texts(), "GPU") || !slices.Contains(tt.Texts(), "63 %") || slices.Contains(tt.Texts(), "Energy") {
		t.Fatalf("texts %q", tt.Texts())
	}
}

func TestIslandHidesItsContentWhileGrowing(t *testing.T) {
	tt := islandTester(macView(), 0.4, &recorder{})
	if slices.Contains(tt.Texts(), "CPU") {
		t.Fatalf("content shown at 40%%: %q", tt.Texts())
	}
}

func TestBarLevels(t *testing.T) {
	cpu, mem, tone := barLevels(macView())
	if cpu < 0.31 || cpu > 0.33 || mem < 0.78 || mem > 0.8 || tone != toneNormal {
		t.Fatalf("levels %v %v %v", cpu, mem, tone)
	}
	v := macView()
	v.Snap.CPU.Total = 95
	if _, _, tone := barLevels(v); tone != toneWarn {
		t.Fatalf("95%% CPU tone %v", tone)
	}
	v.Conn.State = hub.Offline
	if _, _, tone := barLevels(v); tone != toneOffline {
		t.Fatalf("offline tone %v", tone)
	}
	if c, m, _ := barLevels(hub.HostView{}); c != 0 || m != 0 {
		t.Fatalf("no data: %v %v", c, m)
	}
	ui.NewTester(func(c *ui.Context) { Bar(c, macView()) }, int(BarW), 2) // draws without data races or panics
}
