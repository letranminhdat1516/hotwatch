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
	has(t, tt, "RTX 3080", "Usage", "42.0 %", "VRAM", "16.0 %", "1.6 / 10.0 GB", "Temperature", "65.0 °C", "GPU utilisation", "%")
	lacks(t, tt, "—", "No supported GPU on this host")
}

func TestGPUVRAMWithOnlyUsedKnown(t *testing.T) {
	g := rtx
	g.MemTotal = nil
	tt := render(pages.GPU, view(gpuSnap(g), nil, "linux"))
	has(t, tt, "VRAM", "1.6 GB")
	lacks(t, tt, "1.6 / 10.0 GB", "16.0 %")
}

func TestGPUUnknownValuesAreDashes(t *testing.T) {
	tt := render(pages.GPU, view(gpuSnap(model.GPU{Name: "Apple M3"}), nil, "darwin"))
	has(t, tt, "Apple M3", "Temperature", "—")
	lacks(t, tt, "0.0 %", "0.0 °C")
}

func TestGPUEmptyState(t *testing.T) {
	has(t, render(pages.GPU, view(snap(), nil, "linux")), "No supported GPU on this host")
}

func TestGPUManyGPUsNoteAndSections(t *testing.T) {
	second := model.GPU{Name: "RTX 3060", UtilPct: fp(7)}
	tt := render(pages.GPU, view(gpuSnap(rtx, second), nil, "linux"))
	has(t, tt, "RTX 3080", "RTX 3060", "7.0 %", "Chart shows the first GPU (RTX 3080)")
	lacks(t, render(pages.GPU, view(gpuSnap(rtx), nil, "linux")), "Chart shows the first GPU (RTX 3080)")
}

func TestGPUPartialIsUnknown(t *testing.T) {
	s := gpuSnap(rtx)
	s.Partial = []string{"gpu"}
	tt := render(pages.GPU, view(s, nil, "linux"))
	has(t, tt, "—")
	lacks(t, tt, "42.0 %", "65.0 °C", "1.6 / 10.0 GB", "GPU utilisation")
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
	has(t, tt, "87.0 %", "Charging", "Remaining", "1h 15m", "Battery Flow", "12.5 W", "charging",
		"Adapter", "Connected", "Temperature", "31.0 °C", "Thermal", "Nominal", "Awake", "1 assertion",
		"Power", "The battery charges at", "Average", "Peak", "Temperatures",
		"Keeping the Mac awake", "caffeinate", "412", "PreventUserIdleSystemSleep", "Running a build")
	lacks(t, tt, "—", "On battery", "On AC", "Nothing is preventing sleep")
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
	has(t, tt, "55.0 %", "On battery", "Remaining", "Battery Flow", "Adapter", "None", "Temperature", "Thermal", "—", "Nothing is preventing sleep")
	// No power figure: no Power card, no Temperatures card.
	lacks(t, tt, "0.0 W", "0.0 °C", "Average", "Peak", "Temperatures")
}

func TestEnergyNoBattery(t *testing.T) {
	has(t, render(pages.Energy, view(snap(), nil, "darwin")), "No battery on this host")
}

func TestEnergyPartialIsUnknown(t *testing.T) {
	s := energySnap(battery())
	s.Partial = []string{"battery"}
	tt := render(pages.Energy, view(s, nil, "darwin"))
	has(t, tt, "—")
	lacks(t, tt, "87.0 %", "Charging", "12.5 W", "31.0 °C", "Nominal", "caffeinate")
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
