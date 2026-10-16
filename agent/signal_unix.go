//go:build !windows

package agent

import (
	"syscall"

	"hotwatch/model"
)

func systemKill(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }

// systemProtect is nil off Windows: PID 1 and the agent itself are refused
// by Signal already.
var systemProtect func(model.Process) bool
