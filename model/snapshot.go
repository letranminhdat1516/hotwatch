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
	// Partial names sections (cpu, memory, disks, net, gpu, battery) whose
	// values were carried over or unreadable in this sample.
	Partial []string `json:"partial,omitempty"`
}

type CPU struct {
	Total   float64    `json:"total"`
	PerCore []float64  `json:"perCore,omitempty"`
	Load    [3]float64 `json:"load"`
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
}

type GPU struct {
	Name     string   `json:"name"`
	UtilPct  *float64 `json:"utilPct,omitempty"`
	MemUsed  *uint64  `json:"memUsed,omitempty"`
	MemTotal *uint64  `json:"memTotal,omitempty"`
	TempC    *float64 `json:"tempC,omitempty"`
}

type Battery struct {
	ChargePct     float64        `json:"chargePct"`
	Charging      bool           `json:"charging"`
	OnAC          bool           `json:"onAC"`
	TimeRemaining *time.Duration `json:"timeRemaining,omitempty"`
	PowerW        *float64       `json:"powerW,omitempty"`
	TempC         *float64       `json:"tempC,omitempty"`
	ThermalState  string         `json:"thermalState"`
	Assertions    []Assertion    `json:"assertions,omitempty"`
}

// Assertion is a power assertion (something keeping the machine awake).
type Assertion struct {
	PID     int32  `json:"pid"`
	Process string `json:"process"`
	Type    string `json:"type"`
	Reason  string `json:"reason"`
}
