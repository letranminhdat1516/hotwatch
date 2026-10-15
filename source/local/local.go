// Package local is the source.Source for the Mac the app runs on. All calls
// into the (not concurrency-safe) collectors are serialised by one mutex.
package local

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"syscall"
	"time"

	"hotwatch/collect"
	"hotwatch/model"
	"hotwatch/source"
)

// Deps are the platform pieces Local is built from; New (darwin) wires the
// real ones and tests supply fakes.
type Deps struct {
	Collector collect.Collector
	GPUs      func(ctx context.Context) ([]model.GPU, error)    // nil: no GPU source
	Battery   func(ctx context.Context) (*model.Battery, error) // nil: no battery source
	Hostname  func() (string, error)
	Arch      string
	Kernel    func() (string, error)
	Machine   func() model.Machine // nil: nothing for the About page
	// StartTime returns the start time of one process, in the same form the
	// collector reports it; it must return ErrNoProcess when pid is not running.
	StartTime func(ctx context.Context, pid int32) (time.Time, error)
	Kill      func(pid int32, sig model.Signal) error
	Self      int // the app's own pid; never signalled
}

// Local implements source.Source for this machine.
type Local struct {
	d  Deps
	mu sync.Mutex // serialises every call into d.Collector, d.GPUs and d.Battery
}

var _ source.Source = (*Local)(nil)

// ErrNoProcess is returned by Deps.StartTime for a pid that is not running.
var ErrNoProcess = errors.New("no such process")

// NewWith returns a Local over d.
func NewWith(d Deps) *Local { return &Local{d: d} }

func (l *Local) gpus(ctx context.Context) ([]model.GPU, error) {
	if l.d.GPUs == nil {
		return nil, nil
	}
	return l.d.GPUs(ctx)
}

func (l *Local) battery(ctx context.Context) (*model.Battery, error) {
	if l.d.Battery == nil {
		return nil, nil
	}
	return l.d.Battery(ctx)
}

func (l *Local) Info(ctx context.Context) (model.HostInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	info := model.HostInfo{
		ProtocolVersion: model.ProtocolVersion,
		AgentVersion:    "local",
		OS:              "darwin",
		Arch:            l.d.Arch,
		CPUCount:        runtime.NumCPU(),
	}
	// Name and kernel are display facts; a failed lookup leaves them empty.
	if l.d.Hostname != nil {
		info.Hostname, _ = l.d.Hostname()
	}
	if l.d.Kernel != nil {
		info.Kernel, _ = l.d.Kernel()
	}
	if l.d.Machine != nil {
		info.Machine = l.d.Machine()
	}
	gpus, _ := l.gpus(ctx)
	bat, _ := l.battery(ctx)
	info.Caps = model.Caps{GPU: len(gpus) > 0, Battery: bat != nil, Processes: true, Apps: true, Packages: true}
	return info, ctx.Err()
}

// Snapshot is the collector sample plus GPUs and battery. A GPU failure adds
// "gpu" to Partial; a battery failure adds "battery" and keeps whatever
// battery power.Read returned alongside the error.
func (l *Local) Snapshot(ctx context.Context) (model.Snapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, err := l.d.Collector.Sample(ctx)
	if err != nil {
		return model.Snapshot{}, err
	}
	s.Partial = append([]string(nil), s.Partial...)
	gpus, err := l.gpus(ctx)
	if err != nil {
		s.Partial = append(s.Partial, "gpu")
	}
	s.GPUs = gpus
	bat, err := l.battery(ctx)
	if err != nil {
		s.Partial = append(s.Partial, "battery")
	}
	s.Battery = bat
	if len(s.Partial) == 0 {
		s.Partial = nil
	}
	return s, ctx.Err()
}

func (l *Local) Processes(ctx context.Context) ([]model.Process, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.d.Collector.Processes(ctx)
}

// Signal terminates pid only if it is still the process started at start.
// pid <= 1 and the app itself are refused.
func (l *Local) Signal(ctx context.Context, pid int32, start time.Time, sig model.Signal) error {
	if sig != model.SigTerm && sig != model.SigKill {
		return &model.APIError{Code: "bad_request", Message: "unsupported signal " + string(sig)}
	}
	if pid <= 1 || int(pid) == l.d.Self {
		return &model.APIError{Code: "forbidden", Message: "refusing to signal this process"}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// Look up just this pid: a full process listing is slow and disturbs the
	// collector's CPU baseline.
	got, err := l.d.StartTime(ctx, pid)
	switch {
	case errors.Is(err, ErrNoProcess):
		return &model.APIError{Code: "not_found", Message: "process is no longer running"}
	case err != nil:
		return err
	case !got.Equal(start):
		return &model.APIError{Code: "conflict", Message: "pid now belongs to a different process"}
	}
	switch err := l.d.Kill(pid, sig); {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EPERM):
		return &model.APIError{Code: "forbidden", Message: "not permitted to signal this process"}
	case errors.Is(err, syscall.ESRCH):
		return &model.APIError{Code: "not_found", Message: "process is no longer running"}
	default:
		return err
	}
}

var errNoDocker = &model.APIError{Code: "not_found", Message: "Docker is not monitored on this Mac"}

func (l *Local) Containers(context.Context) ([]model.Container, error) { return nil, errNoDocker }

func (l *Local) ContainerAction(context.Context, string, model.ContainerAction) error {
	return errNoDocker
}

func (l *Local) ContainerLogs(context.Context, string, int, bool) (<-chan model.LogLine, error) {
	return nil, errNoDocker
}
