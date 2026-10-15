package main

import (
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"hotwatch/model"
)

const dockerRecheck = 30 * time.Second

// dockerProbe reports whether the Docker socket accepts connections, caching
// the answer for dockerRecheck.
type dockerProbe struct {
	socket string
	now    func() time.Time
	dial   func(socket string) bool

	mu   sync.Mutex
	at   time.Time
	last bool
}

func newDockerProbe(socket string) *dockerProbe {
	return &dockerProbe{socket: socket, now: time.Now, dial: func(s string) bool {
		c, err := net.DialTimeout("unix", s, time.Second)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}}
}

func (p *dockerProbe) Reachable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.at.IsZero() && p.now().Sub(p.at) < dockerRecheck {
		return p.last
	}
	p.last, p.at = p.dial(p.socket), p.now()
	return p.last
}

func hasNvidiaSMI() bool {
	_, err := exec.LookPath("nvidia-smi")
	return err == nil
}

// hostInfo builds the /v1/info provider. ReadOnly and ProtocolVersion are set
// by the server.
func hostInfo(docker *dockerProbe, gpu func() bool) func() model.HostInfo {
	host, _ := os.Hostname()
	kernel := ""
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		kernel = strings.TrimSpace(string(b))
	}
	return func() model.HostInfo {
		return model.HostInfo{
			AgentVersion: version, Hostname: host, OS: "linux", Arch: runtime.GOARCH,
			Kernel: kernel, CPUCount: runtime.NumCPU(),
			Caps: model.Caps{Docker: docker.Reachable(), GPU: gpu(), Processes: true},
		}
	}
}
