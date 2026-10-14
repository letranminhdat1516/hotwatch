//go:build darwin

package local

import (
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
		Arch:      runtime.GOARCH,
		Kernel:    func() (string, error) { return unix.Sysctl("kern.osrelease") },
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
