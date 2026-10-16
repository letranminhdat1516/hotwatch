//go:build darwin

package local

import (
	"context"
	"errors"
	"time"

	gproc "github.com/shirou/gopsutil/v4/process"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"

	darwin "hotwatch/collect/darwin"
	"hotwatch/gpu/apple"
	"hotwatch/model"
	"hotwatch/power"
)

// New returns the Local source backed by the real macOS collectors.
func New() *Local {
	return NewWith(Deps{
		Collector: darwin.New(),
		GPUs:      apple.Read,
		Battery:   power.Read,
		Hostname:  os.Hostname,
		OS:        "darwin",
		MacApps:   true,
		Machine:   darwin.Machine,
		Arch:      runtime.GOARCH,
		Kernel:    func() (string, error) { return unix.Sysctl("kern.osrelease") },
		StartTime: func(ctx context.Context, pid int32) (time.Time, error) {
			p, err := gproc.NewProcessWithContext(ctx, pid)
			if err != nil {
				if errors.Is(err, gproc.ErrorProcessNotRunning) {
					return time.Time{}, ErrNoProcess
				}
				return time.Time{}, err
			}
			ct, err := p.CreateTimeWithContext(ctx)
			if err != nil {
				return time.Time{}, ErrNoProcess // exited, or not identifiable
			}
			return time.UnixMilli(ct), nil // same form as collect/darwin
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
