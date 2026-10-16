package local

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/host"
	"golang.org/x/sys/windows"

	"hotwatch/collect/portable"
	"hotwatch/gpu/nvidia"
	"hotwatch/model"
)

// New returns the Local source backed by the portable collector, the
// Windows battery and NVIDIA GPUs (when nvidia-smi is installed).
func New() *Local {
	return NewWith(Deps{
		Collector: portable.New(),
		GPUs:      nvidiaGPUs,
		Battery:   portable.Battery,
		Hostname:  os.Hostname,
		OS:        "windows",
		Arch:      runtime.GOARCH,
		Kernel:    host.KernelVersion,
		StartTime: func(ctx context.Context, pid int32) (time.Time, error) {
			p, ok, err := portable.Lookup(ctx, pid)
			switch {
			case err != nil:
				return time.Time{}, err
			case !ok:
				return time.Time{}, ErrNoProcess
			case model.WindowsCritical(p.Name):
				return time.Time{}, &model.APIError{Code: "forbidden", Message: "refusing to end a process the system depends on"}
			}
			return p.StartTime, nil
		},
		Kill: terminate,
		Self: os.Getpid(),
	})
}

// terminate ends pid with TerminateProcess: Windows has no TERM for other
// processes, so both signals end it.
func terminate(pid int32, _ model.Signal) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return &model.APIError{Code: "not_found", Message: "process is no longer running"}
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return &model.APIError{Code: "forbidden", Message: "not permitted (run Hotwatch as administrator to end this process)"}
		}
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

func nvidiaGPUs(ctx context.Context) ([]model.GPU, error) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil, nil
	}
	return nvidia.Read(ctx, nvidia.Run)
}
