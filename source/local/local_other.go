//go:build !darwin && !linux && !windows

package local

import (
	"context"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/host"

	"hotwatch/collect/portable"
	"hotwatch/model"
)

// New returns the Local source backed by the portable collector (FreeBSD
// and other systems).
func New() *Local {
	return NewWith(Deps{
		Collector: portable.New(),
		Hostname:  os.Hostname,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Kernel:    host.KernelVersion,
		StartTime: func(ctx context.Context, pid int32) (time.Time, error) {
			p, ok, err := portable.Lookup(ctx, pid)
			if err == nil && !ok {
				err = ErrNoProcess
			}
			return p.StartTime, err
		},
		Kill: func(pid int32, sig model.Signal) error {
			s := syscall.SIGTERM
			if sig == model.SigKill {
				s = syscall.SIGKILL
			}
			return syscall.Kill(int(pid), s)
		},
		Self: os.Getpid(),
	})
}
