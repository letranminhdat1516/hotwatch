package pages_test

import (
	"fmt"
	"image/png"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/model"
	hwui "hotwatch/ui"
	"hotwatch/ui/components"
	"hotwatch/ui/pages"
)

var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func fp(v float64) *float64 { return &v }

func hist(snap model.Snapshot) *hub.History {
	h := hub.NewHistory()
	for i := 60; i >= 0; i-- {
		s := snap
		s.CPU.Total = 20 + float64(i%30)
		h.Push(now.Add(-time.Duration(i)*5*time.Second), s)
	}
	return h
}

func cores(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(5 + i*5)
	}
	return out
}

func procs() []model.Process {
	return []model.Process{
		{PID: 101, Name: "python3", CPUPct: 55, RSS: 900 << 20, DiskReadBps: fp(2048), DiskWriteBps: fp(0), Ports: []model.Port{{Number: 8765, Proto: "tcp"}}},
		{PID: 102, Name: "postgres", CPUPct: 12.5, RSS: 2 << 30, DiskReadBps: nil, DiskWriteBps: fp(4096), Ports: []model.Port{{Number: 5432, Proto: "tcp"}}},
		{PID: 103, Name: "unreadable", CPUPct: 3, RSS: 0},
		{PID: 104, Name: "mDNSResponder", CPUPct: 1, RSS: 20 << 20, Ports: []model.Port{{Number: 5353, Proto: "udp"}, {Number: 22, Proto: "tcp"}}, AppBundle: "com.apple.mdns"},
	}
}

func snap() *model.Snapshot {
	return &model.Snapshot{
		Time: now,
		CPU:  model.CPU{Total: 41.2, PerCore: cores(16), Load: [3]float64{1.5, 1.2, 0.9}},
		Memory: model.Memory{Total: 16 << 30, Used: 10 << 30, App: 6 << 30, Wired: 3 << 30, Compressed: 1 << 30,
			SwapTotal: 4 << 30, SwapUsed: 1 << 30, Pressure: "warn"},
		Disks: []model.Volume{
			{Mount: "/", FS: "apfs", Total: 500 << 30, Used: 250 << 30, ReadBps: fp(1 << 20), WriteBps: nil},
			{Mount: "/data", FS: "ext4", Total: 1 << 40, Used: 100 << 30},
		},
		Net: []model.Interface{
			{Name: "lo0", Addrs: []string{"127.0.0.1"}, Up: true, RxBps: fp(1), TxBps: fp(1)},
			{Name: "en0", Addrs: []string{"192.168.1.20", "fe80::1"}, Up: true, RxBps: fp(5 << 20), TxBps: nil},
			{Name: "utun3", Up: false},
		},
	}
}

func view(s *model.Snapshot, ps []model.Process, os string) hub.HostView {
	v := hub.HostView{Info: model.HostInfo{OS: os}, Snap: s, Procs: ps}
	if s != nil {
		v.Hist = hist(*s)
	}
	return v
}

type page func(*ui.Context, hub.HostView, time.Time)

func render(p page, v hub.HostView) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) { p(c, v, now) }, 900, 2000)
}

func has(t *testing.T, tt *ui.Tester, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.Contains(tt.Texts(), w) {
			t.Errorf("missing %q in %q", w, tt.Texts())
		}
	}
}

func lacks(t *testing.T, tt *ui.Tester, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if slices.Contains(tt.Texts(), w) {
			t.Errorf("unexpected %q in %q", w, tt.Texts())
		}
	}
}

// macCPU is a 10-core Apple M5 sample: six efficiency cores first (cpu0–5),
// then four performance cores, as the kernel numbers them.
func macCPU() hub.HostView {
	s := snap()
	s.CPU = model.CPU{Total: 18.2, User: fp(9.5), System: fp(8.7), TempC: fp(54), TempMaxC: fp(60),
		PerCore: []float64{66, 34, 22, 15, 12, 6, 14, 6.9, 5, 2}, Load: [3]float64{7.08, 9.92, 12.3}}
	s.Uptime = 28*24*time.Hour + 9*time.Hour
	s.Battery = &model.Battery{ThermalState: "nominal"}
	v := view(s, procs(), "darwin")
	v.Info.Machine = model.Machine{Chip: "Apple M5", PerfCores: 4, EffCores: 6}
	v.Procs[0].Threads, v.Procs[1].Threads, v.Procs[3].Threads = 4000, 600, 74
	return v
}

func TestCPUPageProcessorLoad(t *testing.T) {
	tt := render(pages.CPU, macCPU())
	has(t, tt, "Processor Load", "Now", "18.2 %", "User", "9.5%", "System", "8.7%",
		"Temperature", "54 °C", "hottest 60 °C", "Average", "Peak", "now")
}

func TestCPUPageGroupsPerformanceAndEfficiencyCores(t *testing.T) {
	tt := render(pages.CPU, macCPU())
	has(t, tt, "Cores", "10 logical processors", "Performance Cores", "Efficiency Cores", "Core 1", "66%", "Core 7", "14%", "Core 10", "2.0%")
	texts := tt.Texts()
	perf, eff, c7, c1 := slices.Index(texts, "Performance Cores"), slices.Index(texts, "Efficiency Cores"), slices.Index(texts, "Core 7"), slices.Index(texts, "Core 1")
	if !(perf < c7 && c7 < eff && eff < c1) {
		t.Fatalf("cores 7–10 must be under Performance and 1–6 under Efficiency: %q", texts)
	}
	lacks(t, tt, "Core 0", "Core 11")
}

func TestCPUPageActivity(t *testing.T) {
	tt := render(pages.CPU, macCPU())
	has(t, tt, "Activity", "Load, last minute", "7.08 · Busy", "Load, 5 minutes", "9.92", "Load, 15 minutes", "12.3",
		"Trend", "Falling", "Processes", "4", "Threads", "4,674", "Uptime", "28d 9h", "Thermal")
}

func TestCPUPageTopProcessesAndShowAll(t *testing.T) {
	var went []string
	pages.SetNavigate(func(p string) { went = append(went, p) })
	defer pages.SetNavigate(nil)
	tt := render(pages.CPU, macCPU())
	has(t, tt, "Using the Most CPU", "python3", "55%", "postgres", "12%", "Show All")
	if err := tt.Click("Show All"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(went, []string{"processes"}) {
		t.Fatalf("navigated %v", went)
	}
}

func TestCPUPageOnALinuxHost(t *testing.T) {
	tt := render(pages.CPU, view(snap(), procs(), "linux"))
	has(t, tt, "Processor Load", "41.2 %", "Cores", "16 logical processors", "Core 1", "Core 16", "Load, last minute", "1.50 · Quiet")
	lacks(t, tt, "Performance Cores", "Efficiency Cores", "Temperature", "Core 17", "Thermal")
}

func TestCPUWithoutProcessesPointsToProcessesPage(t *testing.T) {
	tt := render(pages.CPU, view(snap(), nil, "linux"))
	has(t, tt, "Process details are on the Processes page")
	lacks(t, tt, "Using the Most CPU")
}

func TestCPUPartialIsUnknownNotZero(t *testing.T) {
	s := snap()
	s.Partial = []string{"cpu"}
	tt := render(pages.CPU, view(s, nil, "linux"))
	has(t, tt, "Load, last minute", "—")
	lacks(t, tt, "Core 1", "41.2 %")
}

func TestPagesWaitForData(t *testing.T) {
	for name, p := range map[string]page{"cpu": pages.CPU, "mem": pages.Memory, "disk": pages.Disk, "net": pages.Network} {
		t.Run(name, func(t *testing.T) {
			has(t, render(p, view(nil, nil, "linux")), "Waiting for data…")
		})
	}
}

func TestMemoryDarwinSegments(t *testing.T) {
	tt := render(pages.Memory, view(snap(), procs(), "darwin"))
	has(t, tt, "In use of 16.0 GB", "10.0 GB", "App", "Wired", "Compressed", "Free", "Memory by Type", "in use",
		"Pressure", "Page-ins", "Swap-outs", "Swap", "Written to disk", "1.00 GB", "Swap size", "4.00 GB", "now")
	lacks(t, tt, "Buffers")
}

func TestMemoryPressureAndPaging(t *testing.T) {
	s := snap()
	s.Memory.Pressure, s.Memory.PressurePct = "normal", fp(22)
	s.Memory.PageInsPS, s.Memory.SwapOutsPS = fp(58), fp(0)
	tt := render(pages.Memory, view(s, nil, "darwin"))
	has(t, tt, "✓ Normal", "22%", "pressure", "58 /s", "0 /s")
}

func TestMemoryLinuxSegments(t *testing.T) {
	s := snap()
	s.Memory = model.Memory{Total: 16 << 30, Used: 6 << 30, Cached: 4 << 30, Buffers: 1 << 30, Pressure: ""}
	tt := render(pages.Memory, view(s, nil, "linux"))
	has(t, tt, "Used", "Cached", "Buffers", "Free")
	lacks(t, tt, "App", "Wired", "Compressed", "Normal", "Warning", "Critical")
}

func TestMemoryPressureBadgeColour(t *testing.T) {
	th := ui.LightTheme()
	for state, want := range map[string]ui.Color{"normal": th.Success, "warn": th.Warning, "critical": th.Danger} {
		if got := components.StateColor(th, state); got != want {
			t.Errorf("%s colour %v, want %v", state, got, want)
		}
	}
	for state, label := range map[string]string{"normal": "✓ Normal", "warn": "! Elevated", "critical": "! Critical"} {
		s := snap()
		s.Memory.Pressure = state
		has(t, render(pages.Memory, view(s, nil, "darwin")), label)
	}
}

func TestMemorySwapOff(t *testing.T) {
	s := snap()
	s.Memory.SwapTotal, s.Memory.SwapUsed = 0, 0
	has(t, render(pages.Memory, view(s, nil, "darwin")), "Written to disk", "Swap is off or empty.")
}

func TestMemoryByAppGroupsBundles(t *testing.T) {
	ps := []model.Process{
		{PID: 1, Name: "Safari", RSS: 300 << 20, AppBundle: "/Applications/Safari.app"},
		{PID: 2, Name: "Safari Web Content", RSS: 500 << 20, AppBundle: "/Applications/Safari.app"},
		{PID: 3, Name: "zsh", RSS: 10 << 20},
		{PID: 4, Name: "zsh", RSS: 5 << 20},
		{PID: 5, Name: "Ghost", RSS: 0, AppBundle: "/Applications/Ghost.app"},
	}
	tt := render(pages.Memory, view(snap(), ps, "darwin"))
	has(t, tt, "Memory by App", "Safari", "800 MB", "2", "zsh", "15.0 MB")
	lacks(t, tt, "Ghost", "Safari.app", "/Applications/Safari.app")
	linux := render(pages.Memory, view(snap(), ps, "linux"))
	lacks(t, linux, "Memory by App")
	lacks(t, render(pages.Memory, view(snap(), nil, "darwin")), "Memory by App")
}

func TestMemoryTopExcludesUnreadableRSS(t *testing.T) {
	tt := render(pages.Memory, view(snap(), procs(), "darwin"))
	has(t, tt, "Using the Most Memory", "postgres", "2.00 GB", "python3")
	lacks(t, tt, "unreadable")
}

func TestDiskNilRatesAreDashes(t *testing.T) {
	tt := render(pages.Disk, view(snap(), procs(), "darwin"))
	has(t, tt, "Disk Activity", "Write", "Read", "Reading and writing", "Reading", "1.00 MB/s", "Writing", "—",
		"/", "APFS", "/data", "EXT4", "Free of 500 GB", "250 GB · 50%")
	has(t, tt, "Reading and Writing the Most", "python3", "postgres")
	lacks(t, tt, "unreadable", "mDNSResponder", "Device")
}

func TestDiskTotalsNamesAndImages(t *testing.T) {
	s := snap()
	s.Disks = []model.Volume{{Mount: "/", FS: "apfs", Device: "disk3", Name: "Macintosh HD", Total: 494 << 30, Used: 364 << 30,
		ReadBps: fp(1 << 20), WriteBps: fp(0), ReadBytes: u64(6300 << 30), WriteBytes: u64(4110 << 30)},
		{Mount: "/Volumes/Pulse", FS: "hfs", Device: "/dev/disk8s1", Name: "Pulse", Total: 19 << 20, Used: 15 << 20, Image: true}}
	tt := render(pages.Disk, view(s, nil, "darwin"))
	has(t, tt, "Read", "6.15 TB", "Written", "4.01 TB", "since startup", "Macintosh HD", "Free of 494 GB", "130 GB",
		"disk3", "Disk Images", "1 mounted", "Pulse", "19.0 MB · hfs")
}

func TestDiskPartialIsUnknown(t *testing.T) {
	s := snap()
	s.Partial = []string{"disks"}
	tt := render(pages.Disk, view(s, nil, "linux"))
	lacks(t, tt, "APFS", "/data")
	has(t, tt, "—")
}

func TestNetworkInterfacesSkipLoopback(t *testing.T) {
	tt := render(pages.Network, view(snap(), procs(), "darwin"))
	has(t, tt, "Downloading", "en0", "IPv4", "192.168.1.20", "5.00 MB/s")
	lacks(t, tt, "lo0", "127.0.0.1", "utun3") // utun3 is down
}

func TestNetworkInterfaceCards(t *testing.T) {
	s := snap()
	s.Net = []model.Interface{
		{Name: "en0", Kind: "wifi", Label: "Wi-Fi", Up: true, Addrs: []string{"fe80::1/64", "192.168.100.225/24"}, MAC: "0a:d0:d1:71:85:ce",
			RxBps: fp(5120), TxBps: fp(30700), RxBytes: u64(1370 << 20), TxBytes: u64(2570 << 20)},
		{Name: "utun8", Kind: "vpn", Label: "VPN", Up: true, Addrs: []string{"100.123.1.44/32"}, RxBps: fp(0), TxBps: fp(0)},
		{Name: "utun7", Kind: "vpn", Label: "VPN", Up: true, Addrs: []string{"100.96.0.27/32"}, RxBps: fp(1000), TxBps: fp(0)},
		{Name: "utun1", Kind: "vpn", Up: true, Addrs: []string{"fe80::9/64"}},
		{Name: "bridge0", Kind: "bridge", Up: true, Addrs: []string{"10.0.0.1/24"}},
	}
	tt := render(pages.Network, view(s, procs(), "darwin"))
	// The tunnels' traffic is not counted twice in the totals.
	has(t, tt, "Downloading", "5.00 KB/s", "Received", "1.34 GB", "since startup", "Wi-Fi", "en0", "VPN", "utun7", "VPN 2", "utun8",
		"Hardware", "0a:d0:d1:71:85:ce", "100.123.1.44")
	lacks(t, tt, "utun1", "bridge0", "6.00 KB/s")
}

func TestNetworkListensSortedByPort(t *testing.T) {
	tt := render(pages.Network, view(snap(), procs(), "darwin"))
	has(t, tt, "Listening Ports", "8765/tcp", "python3", "5353/udp", "22/tcp", "5432/tcp")
	texts := tt.Texts()
	last := -1
	for _, p := range []string{"22/tcp", "5353/udp", "5432/tcp", "8765/tcp"} {
		i := slices.Index(texts, p)
		if i < last {
			t.Fatalf("ports not sorted ascending: %q", texts)
		}
		last = i
	}
}

func TestNetworkWithoutProcessesPointsToProcessesPage(t *testing.T) {
	tt := render(pages.Network, view(snap(), nil, "linux"))
	has(t, tt, "Process details are on the Processes page")
}

// TestPageImages writes one light-theme PNG per page for visual review.
func TestPageImages(t *testing.T) {
	dir := os.Getenv("HOTWATCH_PNG_DIR")
	if dir == "" {
		t.Skip("set HOTWATCH_PNG_DIR to render page images")
	}
	for name, p := range map[string]page{"cpu": pages.CPU, "memory": pages.Memory, "disk": pages.Disk, "network": pages.Network} {
		v := view(snap(), procs(), "darwin")
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(hwui.Theme(false))
			ui.Column(c).Padding(24).Background(c.Theme().Background).Children(func() { p(c, v, now) })
		}, 1000, 1300)
		f, err := os.Create(fmt.Sprintf("%s/hotwatch-e4-%s.png", dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
