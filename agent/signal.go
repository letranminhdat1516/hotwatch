package agent

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"syscall"
	"time"

	"hotwatch/model"
)

// pinned is a handle that signals exactly the process it was opened for,
// immune to PID reuse after opening.
type pinned interface {
	Signal(sig syscall.Signal) error
	Close() error
}

type signaller struct {
	// lookup reads one process. It must not use the sampler's collector
	// concurrently; production reads /proc/<pid>/stat directly.
	lookup func(ctx context.Context, pid int32) (model.Process, bool, error)
	kill   func(pid int, sig syscall.Signal) error
	// pin, if set, opens a stable handle (pidfd) before the final start-time
	// check; errors other than ESRCH fall back to kill.
	pin func(pid int) (pinned, error)
	// protect, if set, refuses processes the OS cannot lose (Windows'
	// csrss, lsass…: ending one stops the machine).
	protect func(model.Process) bool
}

// NewSignaller returns a Signaller that signals with kill(2)-style kill after
// checking the process start time via lookup. There remains a small window
// between the check and kill in which the PID could be reused; use
// NewSystemSignaller to close it where the OS allows.
func NewSignaller(lookup func(ctx context.Context, pid int32) (model.Process, bool, error), kill func(pid int, sig syscall.Signal) error) Signaller {
	return &signaller{lookup: lookup, kill: kill}
}

// NewSystemSignaller signals real processes. On Linux it opens a pidfd, then
// re-checks the start time through it, which removes the PID-reuse window;
// elsewhere, or when pidfd is unsupported, it falls back to kill(2) with the
// small documented race.
func NewSystemSignaller(lookup func(ctx context.Context, pid int32) (model.Process, bool, error)) Signaller {
	return &signaller{lookup: lookup, kill: systemKill, pin: openPin, protect: systemProtect}
}

// windowsCritical refuses the Windows processes whose end stops the machine.
func windowsCritical(p model.Process) bool { return model.WindowsCritical(p.Name) }

// check verifies pid exists and still has the start time the caller saw.
// Start times are compared exactly: the collector and lookup derive them from
// the same clock-tick formula, so equal processes yield equal values.
func (s *signaller) check(ctx context.Context, pid int32, start time.Time) error {
	p, ok, err := s.lookup(ctx, pid)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, fs.ErrNotExist) {
		return &model.APIError{Code: "not_found", Message: "process has exited"}
	}
	if err != nil {
		return &model.APIError{Code: "internal", Message: "cannot read process: " + err.Error()}
	}
	if !ok {
		return &model.APIError{Code: "not_found", Message: "process has exited"}
	}
	if !p.StartTime.Equal(start) {
		return &model.APIError{Code: "conflict", Message: "process start time differs; the PID was reused"}
	}
	if s.protect != nil && s.protect(p) {
		return &model.APIError{Code: "forbidden", Message: "refusing to end a process the system depends on"}
	}
	return nil
}

func (s *signaller) Signal(ctx context.Context, pid int32, start time.Time, sig model.Signal) error {
	if pid <= 1 || int(pid) == os.Getpid() || int(pid) == os.Getppid() {
		return &model.APIError{Code: "forbidden", Message: "refusing to signal this process"}
	}
	var sc syscall.Signal
	switch sig {
	case model.SigTerm:
		sc = syscall.SIGTERM
	case model.SigKill:
		sc = syscall.SIGKILL
	default:
		return &model.APIError{Code: "bad_request", Message: "signal must be TERM or KILL"}
	}
	if err := s.check(ctx, pid, start); err != nil {
		return err
	}
	if s.pin != nil {
		p, err := s.pin(int(pid))
		switch {
		case err == nil:
			defer p.Close()
			// Re-check after the handle exists: from here the handle refers
			// to the process we verified.
			if err := s.check(ctx, pid, start); err != nil {
				return err
			}
			return mapSignalErr(p.Signal(sc))
		case !errors.Is(err, syscall.ENOSYS):
			// Fail closed: EMFILE, EINVAL (a thread id), seccomp EPERM...
			// must not reopen the check/kill race.
			return mapSignalErr(err)
		}
		// Kernel without pidfd_open: fall through to kill.
	}
	return mapSignalErr(s.kill(int(pid), sc))
}

func mapSignalErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EPERM), errors.Is(err, fs.ErrPermission):
		return &model.APIError{Code: "forbidden", Message: "not permitted (process owned by another user)"}
	case errors.Is(err, syscall.ESRCH):
		return &model.APIError{Code: "not_found", Message: "no such process"}
	}
	return &model.APIError{Code: "internal", Message: "signal failed: " + err.Error()}
}
