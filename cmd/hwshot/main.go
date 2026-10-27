// Command hwshot renders the app's window to a PNG with representative
// multi-host data, for iterating on the UI without a live agent. Dev-only.
//
//	go run ./cmd/hwshot /tmp/shot.png [page] [hostIndex] [- ] [dark]
package main

import (
	"image"
	"image/png"
	"os"
	"time"

	mui "github.com/egoist/mygo/ui"

	"hotwatch/hub"
	"hotwatch/mac/appicon"
	"hotwatch/mac/apps"
	"hotwatch/mac/pkgs"
	"hotwatch/model"
	"hotwatch/store"
	hwui "hotwatch/ui"
	"hotwatch/ui/hostpanel"
)

func fp(v float64) *float64 { return &v }
func up(v uint64) *uint64   { return &v }

func hist(s model.Snapshot) *hub.History {
	h := hub.NewHistory()
	for i := 90; i >= 0; i-- {
		t := s.Time.Add(-time.Duration(i) * time.Second)
		wob := float64(i%12) - 6
		cp := s
		cp.CPU.Total = clamp(s.CPU.Total + wob)
		cp.Memory.Used = s.Memory.Used
		if len(cp.GPUs) > 0 && cp.GPUs[0].UtilPct != nil {
			cp.GPUs = append([]model.GPU(nil), s.GPUs...)
			cp.GPUs[0].UtilPct = fp(clamp(gpuVal(s) + wob))
		}
		if len(cp.Net) > 0 {
			cp.Net = append([]model.Interface(nil), s.Net...)
			cp.Net[0].RxBps = fp(4<<20 + float64((i*37)%900)*1024)
		}
		h.Push(t, cp)
	}
	return h
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
func gpuVal(s model.Snapshot) float64 {
	if len(s.GPUs) > 0 && s.GPUs[0].UtilPct != nil {
		return *s.GPUs[0].UtilPct
	}
	return 0
}

func macSnap() *model.Snapshot {
	return &model.Snapshot{
		Time: time.Now(),
		CPU: model.CPU{Total: 32, PerCore: []float64{66, 34, 22, 15, 12, 6, 14, 6.9, 5, 2}, Load: [3]float64{7.08, 9.92, 12.3},
			User: fp(25), System: fp(7), TempC: fp(54), TempMaxC: fp(60)},
		Memory: model.Memory{Total: 24 << 30, Used: 20 << 30, App: 5509 << 20, Wired: 3553 << 20,
			Compressed: 11468 << 20, Cached: 3205 << 20, SwapTotal: 8 << 30, SwapUsed: 6 << 30, Pressure: "warn",
			PressurePct: fp(22), PageInsPS: fp(58), SwapOutsPS: fp(0)},
		Disks: []model.Volume{{Mount: "/", FS: "apfs", Device: "disk3", Name: "Macintosh HD", Total: 494 << 30, Used: 364 << 30,
			ReadBps: fp(573 << 10), WriteBps: fp(33 << 20), ReadBytes: up(6300 << 30), WriteBytes: up(4110 << 30)},
			{Mount: "/Volumes/Pulse", FS: "hfs", Device: "/dev/disk8s1", Name: "Pulse", Total: 19 << 20, Used: 15 << 20, Image: true}},
		Net: []model.Interface{
			{Name: "en0", Kind: "wifi", Label: "Wi-Fi", Addrs: []string{"fe80::43c:4834:3379:bcb1/64", "192.168.100.225/24"}, MAC: "0a:d0:d1:71:85:ce",
				Up: true, RxBps: fp(27 << 10), TxBps: fp(15 << 10), RxBytes: up(1370 << 20), TxBytes: up(2570 << 20)},
			{Name: "utun8", Kind: "vpn", Label: "VPN", Addrs: []string{"100.123.1.44/32", "fd7a:115c:a1e0::c330:12d/48"},
				Up: true, RxBps: fp(0), TxBps: fp(0), RxBytes: up(89 << 20), TxBytes: up(313 << 20)},
			{Name: "utun7", Kind: "vpn", Label: "VPN", Addrs: []string{"100.96.0.27/32", "2606:4700:cf1:1000::16/128"},
				Up: true, RxBps: fp(1 << 10), TxBps: fp(0), RxBytes: up(28 << 10), TxBytes: up(5 << 10)}},
		GPUs:         []model.GPU{{Name: "Apple M5", UtilPct: fp(46), RendererPct: fp(46), TilerPct: fp(14), MemUsed: up(534 << 20), MemAlloc: up(2530 << 20)}},
		StorageTempC: fp(41),
		Battery: &model.Battery{ChargePct: 79, OnAC: true, Charging: true, PowerW: fp(89.6), TempC: fp(31), ThermalState: "nominal",
			TimeRemaining: dur(63 * time.Minute), HealthPct: fp(96.3),
			AdapterW: fp(94), AdapterName: "140W USB-C Power Adapter", SystemW: fp(12.4), Cycles: ip(92), CapacityMAh: ip(6016), DesignMAh: ip(6249),
			Assertions: []model.Assertion{{PID: 501, Process: "WhatsApp", Type: "PreventUserIdleSystemSleep", Reason: "cameracaptured-idleSleepPrevention"},
				{PID: 777, Process: "caffeinate", Type: "PreventUserIdleSystemSleep", Reason: "caffeinate command-line tool"}}},
		Uptime: 28*24*time.Hour + 4*time.Hour,
	}
}

func boxSnap() *model.Snapshot {
	return &model.Snapshot{
		Time: time.Now(),
		CPU:  model.CPU{Total: 71, PerCore: make([]float64, 16), Load: [3]float64{8.4, 7.1, 6.0}, User: fp(60), System: fp(11)},
		Memory: model.Memory{Total: 62 << 30, Used: 41 << 30, App: 36 << 30, Wired: 4 << 30,
			Cached: 10 << 30, SwapTotal: 8 << 30, SwapUsed: 0, Pressure: "normal"},
		Disks:  []model.Volume{{Mount: "/", FS: "ext4", Total: 2 << 40, Used: 900 << 30, ReadBps: fp(12 << 20), WriteBps: fp(4 << 20)}},
		Net:    []model.Interface{{Name: "eth0", Addrs: []string{"100.73.66.67"}, Up: true, RxBps: fp(3 << 20), TxBps: fp(900 << 10)}},
		GPUs:   []model.GPU{{Name: "RTX 3080", UtilPct: fp(63), MemUsed: up(7 << 30), MemTotal: up(10 << 30)}},
		Uptime: 19 * 24 * time.Hour,
	}
}

func vpsSnap() *model.Snapshot {
	return &model.Snapshot{
		Time: time.Now(),
		CPU:  model.CPU{Total: 9, PerCore: make([]float64, 4), Load: [3]float64{0.3, 0.2, 0.1}},
		Memory: model.Memory{Total: 8 << 30, Used: 3 << 30, App: 2 << 30, Wired: 1 << 30,
			Cached: 2 << 30, SwapTotal: 2 << 30, SwapUsed: 0, Pressure: "normal"},
		Disks:  []model.Volume{{Mount: "/", FS: "ext4", Total: 80 << 30, Used: 34 << 30, ReadBps: fp(1 << 20), WriteBps: fp(512 << 10)}},
		Net:    []model.Interface{{Name: "eth0", Addrs: []string{"167.253.157.57"}, Up: true, RxBps: fp(200 << 10), TxBps: fp(120 << 10)}},
		Uptime: 42 * 24 * time.Hour,
	}
}

func dur(d time.Duration) *time.Duration { return &d }

func mkView(os string, caps model.Caps, s *model.Snapshot) hub.HostView {
	return hub.HostView{
		Info: model.HostInfo{OS: os, Caps: caps},
		Conn: hub.Conn{State: hub.Online, LastOK: time.Now()},
		Snap: s, Hist: hist(*s),
	}
}

func main() {
	out := "/tmp/hwshot.png"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	page := "overview"
	if len(os.Args) > 2 {
		page = os.Args[2]
	}
	hosts := []store.Host{
		{ID: "local", Name: "This Mac", Kind: "local", Group: "g-local"},
		{ID: "agent-box", Name: "agent-box", Kind: "agent", Group: "g-compute",
			Conn: &store.Conn{Type: "direct", Address: "100.101.7.12", AgentPort: 7171}},
		{ID: "vps", Name: "VPS (dat)", Kind: "agent", Group: "g-public",
			Conn: &store.Conn{Type: "ssh", SSHHost: "167.253.157.57", SSHPort: 22, SSHUser: "dat", AgentPort: 7171}},
	}
	groups := []store.Group{{ID: "g-local", Name: "Local"}, {ID: "g-compute", Name: "Lab"}, {ID: "g-public", Name: "Servers"}}
	views := map[string]hub.HostView{
		"local":     mkView("darwin", model.Caps{GPU: true, Battery: true, Processes: true, Apps: true, Packages: true}, macSnap()),
		"agent-box": mkView("linux", model.Caps{GPU: true, Docker: true, Processes: true}, boxSnap()),
		"vps":       mkView("linux", model.Caps{Docker: true, Processes: true}, vpsSnap()),
	}
	sel := hosts[0].ID
	if len(os.Args) > 3 {
		if i := os.Args[3][0] - '0'; int(i) < len(hosts) {
			sel = hosts[i].ID
		}
	}
	lv := views["local"]
	lv.Info.Hostname, lv.Info.Arch, lv.Info.Kernel, lv.Info.CPUCount = "cicca-mbp", "arm64", "Darwin 25.3.0", 10
	lv.Info.Machine = model.Machine{Model: "MacBook Pro (14-inch, 2025)", ModelID: "Mac17,1", Chip: "Apple M5",
		PerfCores: 4, EffCores: 6, GPUCores: 10, OSVersion: "26.1", Build: "25B78", Serial: "C02XXXXXXX7K", DisplayW: 3024, DisplayH: 1964, DisplayScale: 2}
	lv.Procs = []model.Process{
		{PID: 1, Name: "Google Chrome", RSS: 8<<30 + 70<<20, AppBundle: "/Applications/Google Chrome.app", User: "cicca", CPUPct: 5.1, Threads: 1055, PowerMW: fp(437), NetRxBps: fp(2230), NetTxBps: fp(400), DiskReadBps: fp(33600), DiskWriteBps: fp(0)},
		{PID: 2, Name: "node", RSS: 3<<30 + 450<<20, User: "cicca", CPUPct: 66.3, Threads: 40, PowerMW: fp(358), NetRxBps: fp(8730), NetTxBps: fp(900), DiskReadBps: fp(831000), DiskWriteBps: fp(12000)},
		{PID: 3, Name: "Docker", RSS: 1<<30 + 380<<20, AppBundle: "/Applications/Docker.app", User: "cicca", CPUPct: 0.7, Threads: 269, PowerMW: fp(83)},
		{PID: 4, Name: "com.apple.WebKit", RSS: 980 << 20, User: "cicca", CPUPct: 1.8, Threads: 24},
		{PID: 5, Name: "Code Helper", RSS: 720 << 20, User: "cicca", CPUPct: 12.3, Threads: 37},
		{PID: 6, Name: "WindowServer", RSS: 109 << 20, User: "_windowserver", CPUPct: 43.8, Threads: 12, PowerMW: fp(65)},
		{PID: 7, Name: "launchd", RSS: 20 << 20, User: "root", CPUPct: 0.2, Threads: 4},
	}
	views["local"] = lv
	now := time.Now()
	vv := views["vps"]
	vv.Log = []hub.LogEntry{
		{Time: now.Add(-3 * time.Hour), State: hub.Connecting, Detail: "Connecting"},
		{Time: now.Add(-3*time.Hour + 2*time.Second), State: hub.Online},
		{Time: now.Add(-40 * time.Minute), State: hub.Stale, Detail: "unavailable: ssh: connect to 167.253.157.57:22: i/o timeout"},
		{Time: now.Add(-39 * time.Minute), State: hub.Offline, Detail: "unavailable: ssh: connect to 167.253.157.57:22: i/o timeout"},
		{Time: now.Add(-35 * time.Minute), State: hub.Online},
	}
	views["vps"] = vv
	a := hwui.NewApp(func(id string) hub.HostView { return views[id] }, hosts, nil, func(string, string) {})
	a.SetHostPanel(hostpanel.New(store.Config{Version: 1, Groups: groups, Hosts: hosts}, nil))
	a.SelectForShot(sel, page)
	a.SetSettings(store.Settings{IntervalMs: 2000})
	// Lucide line icons, as the app draws them.
	if sel == "local" {
		// Real icons: deliveries run inline, so one warm-up render queues the
		// loads and a short wait lets AppKit answer before the real render.
		a.SetIconSource(appicon.NewDarwin(func(f func()) { f() }, 40))
		a.SetPost(func(f func()) { f() })
		a.SetApps(apps.New(nil), nil, nil) // the real scan of this Mac, delivered inline
		a.SetPackages(pkgs.NewManager(nil, nil))
		mui.Render(a.View, 1440, 900, 1)
		time.Sleep(1500 * time.Millisecond)
		if page == "packages" {
			time.Sleep(4 * time.Second) // brew info takes a few seconds
		}
	}

	var img image.Image
	if len(os.Args) > 5 && os.Args[5] == "dark" {
		tt := mui.NewTester(a.View, 1440, 900)
		tt.SetScale(2)
		tt.SetDark(true)
		tt.Frame()
		img = tt.Image()
	} else {
		img = mui.Render(a.View, 1440, 900, 2)
	}
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

func ip(v int) *int { return &v }
