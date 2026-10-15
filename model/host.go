// Package model holds the types shared by the app and the agent.
package model

import (
	"strconv"
	"strings"
)

// ProtocolVersion is the agent API version; only the major part gates compatibility.
const ProtocolVersion = "1.0"

// Compatible reports whether v has the same major version as ProtocolVersion.
func Compatible(v string) bool {
	major, _, _ := strings.Cut(v, ".")
	want, _, _ := strings.Cut(ProtocolVersion, ".")
	return major != "" && major == want
}

type HostInfo struct {
	ProtocolVersion string `json:"protocolVersion"`
	AgentVersion    string `json:"agentVersion"`
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	Kernel          string `json:"kernel"`
	CPUCount        int    `json:"cpuCount"`
	Caps            Caps   `json:"caps"`
	// Machine describes the hardware and OS build for the About page; a
	// host that cannot read a fact leaves it zero and the page omits it.
	Machine Machine `json:"machine,omitzero"`
}

// Machine is what "About This Mac" shows: the model, chip, core split, OS
// build, serial and main display. Every field is optional.
type Machine struct {
	Model        string `json:"model,omitempty"`     // "MacBook Pro (14-inch, 2025)"
	ModelID      string `json:"modelId,omitempty"`   // "Mac17,1"
	Chip         string `json:"chip,omitempty"`      // "Apple M5"
	PerfCores    int    `json:"perfCores,omitempty"` // performance cores
	EffCores     int    `json:"effCores,omitempty"`  // efficiency cores
	GPUCores     int    `json:"gpuCores,omitempty"`
	OSVersion    string `json:"osVersion,omitempty"` // "26.1"
	Build        string `json:"build,omitempty"`     // "25B78"
	Serial       string `json:"serial,omitempty"`
	DisplayW     int    `json:"displayW,omitempty"` // main display, pixels
	DisplayH     int    `json:"displayH,omitempty"`
	DisplayScale int    `json:"displayScale,omitempty"` // 2 on a Retina display
}

type Caps struct {
	GPU       bool `json:"gpu"`
	Battery   bool `json:"battery"`
	Docker    bool `json:"docker"`
	Processes bool `json:"processes"`
	Apps      bool `json:"apps"`
	Packages  bool `json:"packages"`
	ReadOnly  bool `json:"readOnly"`
}

// IncompatibleError reports an agent whose protocol major version differs.
type IncompatibleError struct{ Version string }

func (e *IncompatibleError) Error() string {
	return "agent protocol " + strconv.Quote(e.Version) + " is incompatible with " + strconv.Quote(ProtocolVersion)
}
