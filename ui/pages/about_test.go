package pages_test

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/ui/pages"
)

func TestAboutShowsHardwareSoftwareDisplaysAndStorage(t *testing.T) {
	v := hub.HostView{
		Info: model.HostInfo{Hostname: "cicca-mbp", OS: "darwin", Arch: "arm64", Kernel: "Darwin 25.3.0", CPUCount: 10,
			Machine: model.Machine{Model: "MacBook Pro (14-inch, 2025)", ModelID: "Mac17,1", Chip: "Apple M5",
				PerfCores: 4, EffCores: 6, GPUCores: 10, OSVersion: "26.1", Build: "25B78", Serial: "ABCDEFG7K",
				DisplayW: 3024, DisplayH: 1964}},
		Snap: &model.Snapshot{Memory: model.Memory{Total: 24 << 30}, Uptime: 28*24*time.Hour + 4*time.Hour,
			Disks: []model.Volume{{Mount: "/", FS: "apfs", Device: "disk3", Total: 494 << 30, Used: 480 << 30},
				{Mount: "/System/Volumes/Data", FS: "apfs", Device: "disk3", Total: 494 << 30, Used: 480 << 30}}},
		Procs: make([]model.Process, 623),
	}
	tt := ui.NewTester(func(c *ui.Context) { pages.About(c, v, time.Now()) }, 1200, 900)
	for _, want := range []string{"MacBook Pro (14-inch, 2025)", "Apple M5 · 24.0 GB", "macOS 26.1", "Up 28d 4h",
		"Hardware", "Model identifier", "Mac17,1", "CPU cores", "10 (4 performance, 6 efficiency)", "GPU cores", "arm64",
		"Software", "Build", "25B78", "Kernel", "Darwin 25.3.0", "Computer name", "cicca-mbp", "Processes", "623",
		"Displays", "3024 × 1964", "Storage", "14.0 GB free of 494.0 GB"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if slices.Contains(tt.Texts(), "ABCDEFG7K") {
		t.Error("the serial number is printed in full; it must be hidden until shown")
	}
	if err := tt.Click("Show"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Texts(), "ABCDEFG7K") {
		t.Errorf("serial not revealed after Show: %q", tt.Texts())
	}
}

func TestAboutOmitsWhatTheHostDoesNotReport(t *testing.T) {
	v := hub.HostView{Info: model.HostInfo{OS: "darwin"}, Snap: &model.Snapshot{}}
	tt := ui.NewTester(func(c *ui.Context) { pages.About(c, v, time.Now()) }, 1200, 900)
	for _, absent := range []string{"Model identifier", "GPU cores", "Displays", "Serial"} {
		if slices.Contains(tt.Texts(), absent) {
			t.Errorf("%q shown without data: %q", absent, tt.Texts())
		}
	}
	if !slices.Contains(tt.Texts(), "Hardware") || !slices.Contains(tt.Texts(), "Software") {
		t.Fatalf("cards missing: %q", tt.Texts())
	}
}
