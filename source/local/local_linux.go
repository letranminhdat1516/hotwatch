package local

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"hotwatch/collect/linux"
	"hotwatch/gpu/nvidia"
	"hotwatch/model"
)

// New returns the Local source backed by the /proc collector, NVIDIA GPUs
// (when nvidia-smi is installed) and the sysfs battery.
func New() *Local {
	return NewWith(Deps{
		Collector: linux.New("/"),
		GPUs:      nvidiaGPUs,
		Battery:   func(context.Context) (*model.Battery, error) { return linux.Battery("/") },
		Hostname:  os.Hostname,
		OS:        "linux",
		Arch:      runtime.GOARCH,
		Kernel: func() (string, error) {
			b, err := os.ReadFile("/proc/sys/kernel/osrelease")
			return strings.TrimSpace(string(b)), err
		},
		StartTime: func(_ context.Context, pid int32) (time.Time, error) {
			st, ok, err := linux.ProcessStart("/", pid)
			if err == nil && !ok {
				err = ErrNoProcess
			}
			return st, err
		},
		Kill: unixKill,
		Self: os.Getpid(),
	})
}

func unixKill(pid int32, sig model.Signal) error {
	s := syscall.SIGTERM
	if sig == model.SigKill {
		s = syscall.SIGKILL
	}
	return syscall.Kill(int(pid), s)
}

func nvidiaGPUs(ctx context.Context) ([]model.GPU, error) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil, nil
	}
	return nvidia.Read(ctx, nvidia.Run)
}
