package agent

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// Windows has no signals for other processes: TERM and KILL both end the
// process with TerminateProcess. The handle from OpenProcess pins the
// process, so the start time re-checked after opening it cannot belong to a
// reused PID.

// exitTerminated is the exit code a process ended by the agent reports.
const exitTerminated = 1

type procHandle windows.Handle

func (h procHandle) Signal(syscall.Signal) error {
	return windows.TerminateProcess(windows.Handle(h), exitTerminated)
}
func (h procHandle) Close() error { return windows.CloseHandle(windows.Handle(h)) }

func openPin(pid int) (pinned, error) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil, syscall.ESRCH // no process with that id
		}
		return nil, err
	}
	return procHandle(h), nil
}

func systemKill(pid int, sig syscall.Signal) error {
	p, err := openPin(pid)
	if err != nil {
		return err
	}
	defer p.Close()
	return p.Signal(sig)
}

var systemProtect = windowsCritical
