package pages_test

import (
	"fmt"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/model"
	hwui "hotwatch/ui"
	"hotwatch/ui/components"
	"hotwatch/ui/pages"
)

func ip(v int) *int { return &v }

func u64(v uint64) *uint64 { return &v }

func dur(d time.Duration) *time.Duration { return &d }

func gpuSnap(gs ...model.GPU) *model.Snapshot {
	s := snap()
	s.GPUs = gs
	return s
}

var rtx = model.GPU{Name: "RTX 3080", UtilPct: fp(42), MemUsed: u64(1717986918), MemTotal: u64(10 << 30), TempC: fp(65)}

func TestGPUShowsUtilisationVRAMAndTemperature(t *testing.T) {
	tt := render(pages.GPU, view(gpuSnap(rtx), nil, "linux"))
	has(t, tt, "Graphics", "RTX 3080", "42 %", "Memory", "1.60 / 10.0 GB", "Temperature", "65 °C", "Peak",
		"About", "Video memory", "10.0 GB", "Engines", "Device", "42%", "16%")
	lacks(t, tt, "No supported GPU on this host", "Unified memory")
}

func TestGPUVRAMWithOnlyUsedKnown(t *testing.T) {
	g := rtx
	g.MemTotal = nil
	tt := render(pages.GPU, view(gpuSnap(g), nil, "linux"))
	has(t, tt, "Memory in use", "1.60 GB")
	lacks(t, tt, "1.60 / 10.0 GB", "16%", "Video memory")
}

func TestGPUUnknownValuesAreDashes(t *testing.T) {
	tt := render(pages.GPU, view(gpuSnap(model.GPU{Name: "Apple M3"}), nil, "darwin"))
	has(t, tt, "Apple M3", "—", "Unified memory")
	lacks(t, tt, "0 %", "0 °C", "Renderer", "Tiler")
}

func TestGPUAppleEngines(t *testing.T) {
	m5 := model.GPU{Name: "Apple M5", UtilPct: fp(46), RendererPct: fp(46), TilerPct: fp(14), MemUsed: u64(534 << 20), MemAlloc: u64(2530 << 20)}
	v := view(gpuSnap(m5), nil, "darwin")
	v.Info.Machine.GPUCores = 10
	tt := render(pages.GPU, v)
	has(t, tt, "46 %", "Renderer", "46%", "Tiler", "14%", "534 MB", "Cores", "10", "Memory allocated", "2.47 GB", "Unified memory")
}

func TestGPUEmptyState(t *testing.T) {
	has(t, render(pages.GPU, view(snap(), nil, "linux")), "No supported GPU on this host")
}

func TestGPUManyGPUsNoteAndSections(t *testing.T) {
	second := model.GPU{Name: "RTX 3060", UtilPct: fp(7)}
	tt := render(pages.GPU, view(gpuSnap(rtx, second), nil, "linux"))
	has(t, tt, "RTX 3080", "RTX 3060", "7 %")
}

func TestGPUPartialIsUnknown(t *testing.T) {
	s := gpuSnap(rtx)
	s.Partial = []string{"gpu"}
	tt := render(pages.GPU, view(s, nil, "linux"))
	has(t, tt, "—")
	lacks(t, tt, "42 %", "65 °C", "1.60 / 10.0 GB")
}

func TestGPUWaitsForData(t *testing.T) {
	has(t, render(pages.GPU, view(nil, nil, "linux")), "Waiting for data…")
	has(t, render(pages.Energy, view(nil, nil, "linux")), "Waiting for data…")
}

func battery() *model.Battery {
	return &model.Battery{ChargePct: 87, Charging: true, OnAC: true, TimeRemaining: dur(75 * time.Minute),
		PowerW: fp(12.5), TempC: fp(31), ThermalState: "nominal",
		Assertions: []model.Assertion{{PID: 412, Process: "caffeinate", Type: "PreventUserIdleSystemSleep", Reason: "Running a build"}}}
}

func energySnap(b *model.Battery) *model.Snapshot {
	s := snap()
	s.Battery = b
	return s
}

func TestEnergyShowsChargePowerThermalAndAssertions(t *testing.T) {
	tt := render(pages.Energy, view(energySnap(battery()), nil, "darwin"))
	has(t, tt, "87%", "Charging", "Until Full", "1h 15m", "Battery Flow", "12.5 W", "charging",
		"Adapter", "Connected", "Nominal", "Power", "The battery's flow", "Average", "Peak", "Thermal", "Cool",
		"Temperatures", "Battery", "31 °C", "Keeping Awake", "caffeinate", "Running a build", "● System",
		"Power Source", "Drawing from", "Power Adapter", "87% · Charging")
	lacks(t, tt, "On battery", "Nothing is keeping the Mac awake.")
}

func TestEnergyAdapterHealthAndWholeComputer(t *testing.T) {
	b := battery()
	b.AdapterW, b.AdapterName, b.SystemW = fp(94), "140W USB-C Power Adapter", fp(12.4)
	b.HealthPct, b.Cycles, b.CapacityMAh, b.DesignMAh = fp(96.3), ip(92), ip(6016), ip(6249)
	s := energySnap(b)
	s.StorageTempC = fp(41)
	tt := render(pages.Energy, view(s, nil, "darwin"))
	has(t, tt, "96%", "of design capacity", "Cycles", "92", "6,016 mAh", "design 6,249", "94 W", "140W USB-C Power Adapter",
		"The whole computer draws", "12.4 W", "Storage", "41 °C", "Adapter rating", "Whole computer")
}

func TestEnergyStateBadges(t *testing.T) {
	th := ui.LightTheme()
	for name, tc := range map[string]struct {
		charging, ac bool
		label        string
		state        string
		want         ui.Color
	}{
		"charging":   {true, true, "Charging", "charging", th.Success},
		"on battery": {false, false, "On battery", "on battery", th.Warning},
		"on ac":      {false, true, "On AC", "on ac", th.TextMuted},
	} {
		t.Run(name, func(t *testing.T) {
			b := battery()
			b.Charging, b.OnAC = tc.charging, tc.ac
			has(t, render(pages.Energy, view(energySnap(b), nil, "darwin")), tc.label)
			if got := components.StateColor(th, tc.state); got != tc.want {
				t.Errorf("colour %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEnergyThermalBadges(t *testing.T) {
	th := ui.LightTheme()
	for state, tc := range map[string]struct {
		label string
		want  ui.Color
	}{
		"nominal":  {"Nominal", th.Success},
		"fair":     {"Fair", th.Warning},
		"serious":  {"Serious", th.Danger},
		"critical": {"Critical", th.Danger},
	} {
		b := battery()
		b.ThermalState = state
		has(t, render(pages.Energy, view(energySnap(b), nil, "darwin")), tc.label)
		if got := components.StateColor(th, state); got != tc.want {
			t.Errorf("%s colour %v, want %v", state, got, tc.want)
		}
	}
}

func TestEnergyUnknownValuesAreDashes(t *testing.T) {
	b := &model.Battery{ChargePct: 55}
	tt := render(pages.Energy, view(energySnap(b), nil, "darwin"))
	has(t, tt, "55%", "On battery", "Remaining", "Battery Flow", "Adapter", "None", "—", "Nothing is keeping the Mac awake.")
	// No power figure: no Power card; no temperature: no Temperatures card.
	lacks(t, tt, "0.0 W", "0 °C", "Average", "Peak", "Temperatures")
}

func TestEnergyNoBattery(t *testing.T) {
	has(t, render(pages.Energy, view(snap(), nil, "darwin")), "No battery on this host")
}

func TestEnergyPartialIsUnknown(t *testing.T) {
	s := energySnap(battery())
	s.Partial = []string{"battery"}
	tt := render(pages.Energy, view(s, nil, "darwin"))
	has(t, tt, "—")
	lacks(t, tt, "87%", "Charging", "12.5 W", "31 °C", "Nominal", "caffeinate")
}

// TestEnergyGPUImages writes the GPU and Energy pages as PNGs for review.
func TestEnergyGPUImages(t *testing.T) {
	dir := os.Getenv("HOTWATCH_E8_PNG_DIR")
	if dir == "" {
		t.Skip("set HOTWATCH_E8_PNG_DIR to render page images")
	}
	second := model.GPU{Name: "RTX 3060", UtilPct: fp(7), MemUsed: u64(2 << 30)}
	cases := map[string]struct {
		p page
		s *model.Snapshot
	}{"gpu": {pages.GPU, gpuSnap(rtx, second)}, "energy": {pages.Energy, energySnap(battery())}}
	for name, tc := range cases {
		v := view(tc.s, nil, "darwin")
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(hwui.Theme(false))
			ui.Column(c).Padding(24).Background(c.Theme().Background).Children(func() { tc.p(c, v, now) })
		}, 1000, 900)
		f, err := os.Create(fmt.Sprintf("%s/hotwatch-e8-%s.png", dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
