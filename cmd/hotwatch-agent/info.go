package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"hotwatch/model"
)

const dockerRecheck = 30 * time.Second

// dockerProbe reports whether the Docker daemon accepts connections,
// caching the answer for dockerRecheck.
type dockerProbe struct {
	now  func() time.Time
	dial func() bool

	mu   sync.Mutex
	at   time.Time
	last bool
}

// newDockerProbe probes with dial, which opens one connection to the daemon.
func newDockerProbe(dial func(ctx context.Context) (net.Conn, error)) *dockerProbe {
	return &dockerProbe{now: time.Now, dial: func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c, err := dial(ctx)
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
	p.last, p.at = p.dial(), p.now()
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
	kernel := kernelVersion()
	return func() model.HostInfo {
		return model.HostInfo{
			AgentVersion: version, Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH,
			Kernel: kernel, CPUCount: runtime.NumCPU(),
			Caps: model.Caps{Docker: docker.Reachable(), GPU: gpu(), Processes: true},
		}
	}
}
