package model

import "time"

// Signal is a process signal the agent accepts.
type Signal string

const (
	SigTerm Signal = "TERM"
	SigKill Signal = "KILL"
)

// Process: (PID, StartTime) identifies a process and guards against PID reuse.
type Process struct {
	PID       int32     `json:"pid"`
	PPID      int32     `json:"ppid"`
	StartTime time.Time `json:"startTime"`
	Name      string    `json:"name"`
	User      string    `json:"user"`
	Command   string    `json:"command"` // argv joined; never environment
	CPUPct    float64   `json:"cpuPct"`
	RSS       uint64    `json:"rss"`
	// Footprint is the physical footprint (macOS: ri_phys_footprint, which
	// counts the process's compressed pages; Activity Monitor's "Memory").
	// 0: not readable (other users' processes) or not reported (Linux).
	Footprint    uint64   `json:"footprint,omitempty"`
	Threads      int32    `json:"threads"`
	DiskReadBps  *float64 `json:"diskReadBps,omitempty"` // nil = not readable
	DiskWriteBps *float64 `json:"diskWriteBps,omitempty"`
	Ports        []Port   `json:"ports,omitempty"`     // listening
	AppBundle    string   `json:"appBundle,omitempty"` // macOS only
}

type Port struct {
	Number uint16 `json:"number"`
	Proto  string `json:"proto"` // "tcp" or "udp"
}

// Mem is the memory to show for p: its footprint where known, else RSS.
// 0 means unknown.
func (p *Process) Mem() uint64 {
	if p.Footprint > 0 {
		return p.Footprint
	}
	return p.RSS
}
