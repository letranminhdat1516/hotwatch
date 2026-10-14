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
	PID          int32     `json:"pid"`
	PPID         int32     `json:"ppid"`
	StartTime    time.Time `json:"startTime"`
	Name         string    `json:"name"`
	User         string    `json:"user"`
	Command      string    `json:"command"` // argv joined; never environment
	CPUPct       float64   `json:"cpuPct"`
	RSS          uint64    `json:"rss"`
	Threads      int32     `json:"threads"`
	DiskReadBps  *float64  `json:"diskReadBps,omitempty"` // nil = not readable
	DiskWriteBps *float64  `json:"diskWriteBps,omitempty"`
	Ports        []Port    `json:"ports,omitempty"`     // listening
	AppBundle    string    `json:"appBundle,omitempty"` // macOS only
}

type Port struct {
	Number uint16 `json:"number"`
	Proto  string `json:"proto"` // "tcp" or "udp"
}
