// Package model holds the types shared by the app and the agent.
package model

import "strings"

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
