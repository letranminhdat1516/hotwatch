package portable

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"

	"hotwatch/model"
)

var bg = context.Background()

// fakeAPI serves scripted readings; each call to step advances the clock
// and the counters.
type fakeAPI struct {
	now   time.Time
	cores [][]cpu.TimesStat // one reading per sample
	disk  []map[string]disk.IOCountersStat
	net   [][]gnet.IOCountersStat
	procs [][]procInfo
	ports map[int32][]model.Port
	i     int
}

func (f *fakeAPI) api() api {
	pick := func(n int) int { return min(f.i, n-1) }
	return api{
		now:      func() time.Time { return f.now },
		cpuTimes: func(context.Context) ([]cpu.TimesStat, error) { return f.cores[pick(len(f.cores))], nil },
		vmem: func(context.Context) (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 16 << 30, Available: 1 << 30, Used: 15 << 30, Cached: 3 << 30}, nil
		},
		swap: func(context.Context) (*mem.SwapMemoryStat, error) {
			return &mem.SwapMemoryStat{Total: 4 << 30, Used: 1 << 30}, nil
		},
		uptime: func(context.Context) (uint64, error) { return 3600, nil },
		partitions: func(context.Context) ([]disk.PartitionStat, error) {
			return []disk.PartitionStat{{Device: "C:", Mountpoint: `C:\`, Fstype: "NTFS"}, {Device: "D:", Mountpoint: `D:\`, Fstype: "CDFS"}}, nil
		},
		usage: func(_ context.Context, p string) (*disk.UsageStat, error) {
			if p == `D:\` {
				return nil, errors.New("not ready") // an empty optical drive
			}
			return &disk.UsageStat{Path: p, Total: 500 << 30, Used: 200 << 30}, nil
		},
		diskIO: func(context.Context) (map[string]disk.IOCountersStat, error) { return f.disk[pick(len(f.disk))], nil },
		netIO:  func(context.Context) ([]gnet.IOCountersStat, error) { return f.net[pick(len(f.net))], nil },
		netIfs: func(context.Context) (gnet.InterfaceStatList, error) {
			return gnet.InterfaceStatList{
				{Name: "Wi-Fi", Flags: []string{"up"}, HardwareAddr: "aa:bb:cc:dd:ee:ff", Addrs: gnet.InterfaceAddrList{{Addr: "192.168.1.5/24"}}},
				{Name: "Tailscale", Flags: []string{"up"}, Addrs: gnet.InterfaceAddrList{{Addr: "100.64.0.9/32"}}},
				{Name: "Loopback Pseudo-Interface 1", Flags: []string{"up", "loopback"}},
				{Name: "vEthernet (WSL)", Flags: []string{"up"}},
				{Name: "Ethernet 2"},
			}, nil
		},
		procs: func(context.Context, map[procKey]procStatic) ([]procInfo, error) {
			return f.procs[pick(len(f.procs))], nil
		},
		listeners: func(context.Context) (map[int32][]model.Port, error) { return f.ports, nil },
	}
}

func (f *fakeAPI) step(d time.Duration) { f.now = f.now.Add(d); f.i++ }

func core(user, system, idle float64) cpu.TimesStat {
	return cpu.TimesStat{User: user, System: system, Idle: idle}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSampleCPUIsTheDeltaBetweenReadings(t *testing.T) {
	f := &fakeAPI{now: time.Unix(1000, 0), cores: [][]cpu.TimesStat{
		{core(10, 10, 80), core(0, 0, 100)},
		{core(40, 20, 90), core(0, 0, 200)}, // core 0: 40 busy of 50; core 1 idle
	}, disk: []map[string]disk.IOCountersStat{{}}, net: [][]gnet.IOCountersStat{{}}}
	c := newWith(f.api())
	s, err := c.Sample(bg)
	if err != nil || !slices.Contains(s.Partial, "cpu") {
		t.Fatalf("first sample: partial %v err %v (want cpu partial)", s.Partial, err)
	}
	f.step(time.Second)
	s, _ = c.Sample(bg)
	if slices.Contains(s.Partial, "cpu") {
		t.Fatalf("partial %v", s.Partial)
	}
	if !near(s.CPU.PerCore[0], 80) || !near(s.CPU.PerCore[1], 0) || !near(s.CPU.Total, 40.0/150*100) {
		t.Fatalf("cpu %+v", s.CPU)
	}
	if !near(*s.CPU.User, 30.0/150*100) || !near(*s.CPU.System, 10.0/150*100) {
		t.Fatalf("user %v system %v", *s.CPU.User, *s.CPU.System)
	}
}

func TestSampleMemoryUsesAvailable(t *testing.T) {
	f := &fakeAPI{now: time.Unix(1000, 0), cores: [][]cpu.TimesStat{{core(1, 1, 1)}},
		disk: []map[string]disk.IOCountersStat{{}}, net: [][]gnet.IOCountersStat{{}}}
	s, _ := newWith(f.api()).Sample(bg)
	m := s.Memory
	if m.Total != 16<<30 || m.Used != 15<<30 || m.App != 15<<30 || m.Cached != 3<<30 || m.SwapTotal != 4<<30 || m.SwapUsed != 1<<30 {
		t.Fatalf("memory %+v", m)
	}
	if m.Pressure != "critical" { // 1 GB of 16 GB available
		t.Fatalf("pressure %q", m.Pressure)
	}
	if s.Uptime != time.Hour {
		t.Fatalf("uptime %v", s.Uptime)
	}
}

func TestPressureLevels(t *testing.T) {
	for avail, want := range map[uint64]string{50: "normal", 20: "normal", 15: "warn", 5: "critical"} {
		if got := pressure(100, avail); got != want {
			t.Errorf("%d%% available: %q, want %q", avail, got, want)
		}
	}
	if pressure(0, 0) != "" {
		t.Error("unknown total must give no level")
	}
}

func TestSampleDisksSkipUnreadyDrivesAndRateByDevice(t *testing.T) {
	f := &fakeAPI{now: time.Unix(1000, 0), cores: [][]cpu.TimesStat{{core(1, 1, 1)}},
		disk: []map[string]disk.IOCountersStat{
			{"C:": {Name: "C:", ReadBytes: 1000, WriteBytes: 5000}},
			{"C:": {Name: "C:", ReadBytes: 3000, WriteBytes: 5500}},
		}, net: [][]gnet.IOCountersStat{{}}}
	c := newWith(f.api())
	c.Sample(bg)
	f.step(2 * time.Second)
	s, _ := c.Sample(bg)
	if len(s.Disks) != 1 {
		t.Fatalf("disks %+v (the unready D: must be left out)", s.Disks)
	}
	d := s.Disks[0]
	if d.Mount != `C:\` || d.FS != "NTFS" || d.Total != 500<<30 || d.Used != 200<<30 {
		t.Fatalf("disk %+v", d)
	}
	if d.ReadBps == nil || *d.ReadBps != 1000 || *d.WriteBps != 250 || *d.ReadBytes != 3000 {
		t.Fatalf("rates %v %v", d.ReadBps, d.WriteBps)
	}
}

func TestSampleNetKindsAddressesAndRates(t *testing.T) {
	f := &fakeAPI{now: time.Unix(1000, 0), cores: [][]cpu.TimesStat{{core(1, 1, 1)}},
		disk: []map[string]disk.IOCountersStat{{}},
		net: [][]gnet.IOCountersStat{
			{{Name: "Wi-Fi", BytesRecv: 100, BytesSent: 10}, {Name: "Loopback Pseudo-Interface 1", BytesRecv: 9}},
			{{Name: "Wi-Fi", BytesRecv: 1100, BytesSent: 20}, {Name: "Loopback Pseudo-Interface 1", BytesRecv: 99}},
		}}
	c := newWith(f.api())
	c.Sample(bg)
	f.step(time.Second)
	s, _ := c.Sample(bg)
	kinds := map[string]string{}
	var wifi model.Interface
	for _, it := range s.Net {
		kinds[it.Name] = it.Kind
		if it.Name == "Wi-Fi" {
			wifi = it
		}
	}
	want := map[string]string{"Wi-Fi": "wifi", "Tailscale": "vpn", "vEthernet (WSL)": "bridge", "Ethernet 2": "ethernet"}
	if len(kinds) != len(want) {
		t.Fatalf("interfaces %v (loopback must be left out)", kinds)
	}
	for n, k := range want {
		if kinds[n] != k {
			t.Errorf("%s: kind %q, want %q", n, kinds[n], k)
		}
	}
	if !wifi.Up || wifi.MAC != "aa:bb:cc:dd:ee:ff" || !slices.Equal(wifi.Addrs, []string{"192.168.1.5"}) || wifi.Label != "Wi-Fi" {
		t.Fatalf("wifi %+v", wifi)
	}
	if wifi.RxBps == nil || *wifi.RxBps != 1000 || *wifi.TxBps != 10 {
		t.Fatalf("wifi rates %v %v", wifi.RxBps, wifi.TxBps)
	}
}

func TestProcessesCPUFromDeltasAndPorts(t *testing.T) {
	start := time.Unix(500, 0).UnixMilli()
	f := &fakeAPI{now: time.Unix(1000, 0),
		procs: [][]procInfo{
			{{PID: 10, Create: start, Name: "svc.exe", CPUSecs: 1, Read: 0, Write: 0, HaveIO: true}, {PID: 11, Create: start, Name: "gone.exe"}},
			{{PID: 10, Create: start, Name: "svc.exe", CPUSecs: 1.5, Read: 4096, Write: 0, HaveIO: true}},
		},
		ports: map[int32][]model.Port{10: {{Number: 8080, Proto: "tcp"}}}}
	c := newWith(f.api())
	ps, err := c.Processes(bg)
	if err != nil || len(ps) != 2 || ps[0].CPUPct != 0 {
		t.Fatalf("first scan %+v %v", ps, err)
	}
	f.step(time.Second)
	ps, _ = c.Processes(bg)
	if len(ps) != 1 {
		t.Fatalf("second scan %+v", ps)
	}
	p := ps[0]
	if !near(p.CPUPct, 50) || !p.StartTime.Equal(time.UnixMilli(start)) || p.DiskReadBps == nil || *p.DiskReadBps != 4096 {
		t.Fatalf("process %+v", p)
	}
	if len(p.Ports) != 1 || p.Ports[0].Number != 8080 {
		t.Fatalf("ports %+v", p.Ports)
	}
}

func TestProcessesNewIdentityStartsAtZero(t *testing.T) {
	f := &fakeAPI{now: time.Unix(1000, 0), procs: [][]procInfo{
		{{PID: 10, Create: 1, CPUSecs: 5}},
		{{PID: 10, Create: 2, CPUSecs: 9}}, // PID reused by a new process
	}}
	c := newWith(f.api())
	c.Processes(bg)
	f.step(time.Second)
	ps, _ := c.Processes(bg)
	if ps[0].CPUPct != 0 {
		t.Fatalf("a reused PID must not inherit the old CPU time: %v", ps[0].CPUPct)
	}
}
