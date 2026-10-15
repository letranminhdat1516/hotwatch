package model

import "time"

// Snapshot is one sample of a host. Unknown rates and values are nil, never 0.
type Snapshot struct {
	Time    time.Time     `json:"time"`
	CPU     CPU           `json:"cpu"`
	Memory  Memory        `json:"memory"`
	Disks   []Volume      `json:"disks,omitempty"`
	Net     []Interface   `json:"net,omitempty"`
	GPUs    []GPU         `json:"gpus,omitempty"`
	Battery *Battery      `json:"battery,omitempty"` // nil when absent
	Uptime  time.Duration `json:"uptime"`
	// StorageTempC is the internal storage's temperature (macOS: the NAND
	// sensor), °C; nil where unreadable.
	StorageTempC *float64 `json:"storageTempC,omitempty"`
	// Partial names sections (cpu, memory, disks, net, gpu, battery) whose
	// values were carried over or unreadable in this sample.
	Partial []string `json:"partial,omitempty"`
}

type CPU struct {
	Total   float64    `json:"total"`
	PerCore []float64  `json:"perCore,omitempty"`
	Load    [3]float64 `json:"load"`
	// User and System split Total into time in user code and in the kernel,
	// in percent of all CPU time; nil when the collector does not report
	// them (agents older than this field).
	User   *float64 `json:"user,omitempty"`
	System *float64 `json:"system,omitempty"`
	// TempC is the mean of the CPU die sensors and TempMaxC the hottest, in
	// °C; nil where the host has no readable sensors.
	TempC    *float64 `json:"tempC,omitempty"`
	TempMaxC *float64 `json:"tempMaxC,omitempty"`
}

type Memory struct {
	Total      uint64 `json:"total"`
	Used       uint64 `json:"used"`
	App        uint64 `json:"app"`
	Wired      uint64 `json:"wired"`
	Compressed uint64 `json:"compressed"`
	Cached     uint64 `json:"cached"`
	Buffers    uint64 `json:"buffers"`
	SwapTotal  uint64 `json:"swapTotal"`
	SwapUsed   uint64 `json:"swapUsed"`
	Pressure   string `json:"pressure"`
	// PressurePct is how hard the system works to keep memory available,
	// 0–100 (macOS: 100 − kern.memorystatus_level); nil where unknown.
	PressurePct *float64 `json:"pressurePct,omitempty"`
	// PageInsPS and SwapOutsPS are pages read in from files and pages
	// swapped out, per second; nil on the first sample and where unknown.
	PageInsPS  *float64 `json:"pageInsPs,omitempty"`
	SwapOutsPS *float64 `json:"swapOutsPs,omitempty"`
}

// Volume is one mounted file system. Device identifies the storage whose
// capacity it reports: the APFS container (disk3) on macOS, the mounted
// device on Linux. Volumes sharing a Device share Total and Used and report
// the same store's I/O rate, so both are counted once per Device
// (DistinctVolumes).
type Volume struct {
	Mount    string   `json:"mount"`
	FS       string   `json:"fs"`
	Device   string   `json:"device,omitempty"`
	Total    uint64   `json:"total"`
	Used     uint64   `json:"used"`
	ReadBps  *float64 `json:"readBps,omitempty"`
	WriteBps *float64 `json:"writeBps,omitempty"`
	// Name is the volume's own name ("Macintosh HD"); "" where unknown.
	Name string `json:"name,omitempty"`
	// ReadBytes and WriteBytes are the bytes the volume's storage has read
	// and written since the host started; nil where unknown.
	ReadBytes  *uint64 `json:"readBytes,omitempty"`
	WriteBytes *uint64 `json:"writeBytes,omitempty"`
	// Image marks a mounted disk image (.dmg).
	Image bool `json:"image,omitempty"`
}

// DistinctVolumes returns the first volume of each Device, in order, so
// capacity and I/O are summed once per storage. A volume without a Device
// is identified by its Mount; one with neither cannot be matched to
// anything and always counts on its own.
func DistinctVolumes(vols []Volume) []Volume {
	seen := make(map[string]bool, len(vols))
	out := make([]Volume, 0, len(vols))
	for _, v := range vols {
		key := v.Device
		if key == "" && v.Mount != "" {
			key = "mount:" + v.Mount
		}
		if key == "" {
			out = append(out, v)
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	return out
}

type Interface struct {
	Name  string   `json:"name"`
	Addrs []string `json:"addrs,omitempty"`
	RxBps *float64 `json:"rxBps,omitempty"`
	TxBps *float64 `json:"txBps,omitempty"`
	Up    bool     `json:"up"`
	// Kind is "wifi", "ethernet", "vpn", "bridge" or "" when unknown; Label
	// the system's name for it ("Wi-Fi"); MAC its hardware address.
	Kind  string `json:"kind,omitempty"`
	Label string `json:"label,omitempty"`
	MAC   string `json:"mac,omitempty"`
	// RxBytes and TxBytes are the bytes received and sent since the host
	// started; nil where unknown.
	RxBytes *uint64 `json:"rxBytes,omitempty"`
	TxBytes *uint64 `json:"txBytes,omitempty"`
}

type GPU struct {
	Name     string   `json:"name"`
	UtilPct  *float64 `json:"utilPct,omitempty"`
	MemUsed  *uint64  `json:"memUsed,omitempty"`
	MemTotal *uint64  `json:"memTotal,omitempty"`
	TempC    *float64 `json:"tempC,omitempty"`
	// RendererPct and TilerPct are the busy share of the GPU's two engines
	// (Apple GPUs); MemAlloc the memory the driver has allocated. nil where
	// the driver does not report them.
	RendererPct *float64 `json:"rendererPct,omitempty"`
	TilerPct    *float64 `json:"tilerPct,omitempty"`
	MemAlloc    *uint64  `json:"memAlloc,omitempty"`
}

type Battery struct {
	ChargePct     float64        `json:"chargePct"`
	Charging      bool           `json:"charging"`
	OnAC          bool           `json:"onAC"`
	TimeRemaining *time.Duration `json:"timeRemaining,omitempty"`
	PowerW        *float64       `json:"powerW,omitempty"`
	TempC         *float64       `json:"tempC,omitempty"`
	ThermalState  string         `json:"thermalState"`
	// HealthPct is the full-charge capacity in percent of the design
	// capacity; Cycles the charge cycles; CapacityMAh and DesignMAh the two
	// capacities. nil where the battery does not report them.
	HealthPct   *float64    `json:"healthPct,omitempty"`
	Cycles      *int        `json:"cycles,omitempty"`
	CapacityMAh *int        `json:"capacityMAh,omitempty"`
	DesignMAh   *int        `json:"designMAh,omitempty"`
	Assertions  []Assertion `json:"assertions,omitempty"`
	// AdapterW is the connected power adapter's rating and AdapterName its
	// name ("140W USB-C Power Adapter"); SystemW what the whole computer
	// draws. nil / "" where unknown or on battery.
	AdapterW    *float64 `json:"adapterW,omitempty"`
	AdapterName string   `json:"adapterName,omitempty"`
	SystemW     *float64 `json:"systemW,omitempty"`
}

// Assertion is a power assertion (something keeping the machine awake).
type Assertion struct {
	PID     int32  `json:"pid"`
	Process string `json:"process"`
	Type    string `json:"type"`
	Reason  string `json:"reason"`
}
