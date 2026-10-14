package model

import "time"

type ContainerAction string

const (
	ActionStart   ContainerAction = "start"
	ActionStop    ContainerAction = "stop"
	ActionRestart ContainerAction = "restart"
)

type Container struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	State     string    `json:"state"`
	Status    string    `json:"status"`
	Health    string    `json:"health,omitempty"`
	Project   string    `json:"project,omitempty"` // com.docker.compose.project label
	CPUPct    *float64  `json:"cpuPct,omitempty"`
	MemUsed   *uint64   `json:"memUsed,omitempty"`
	MemLimit  *uint64   `json:"memLimit,omitempty"`
	Ports     []string  `json:"ports,omitempty"`
	StartedAt time.Time `json:"startedAt"`
}

type LogLine struct {
	Time   time.Time `json:"time"`
	Stream string    `json:"stream"`
	Text   string    `json:"text"`
}
