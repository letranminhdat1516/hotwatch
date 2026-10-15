//go:build linux

package agent

import (
	"syscall"

	"golang.org/x/sys/unix"
)

type pidfd int

func (f pidfd) Signal(sig syscall.Signal) error { return unix.PidfdSendSignal(int(f), sig, nil, 0) }
func (f pidfd) Close() error                    { return unix.Close(int(f)) }

func openPin(pid int) (pinned, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, err
	}
	return pidfd(fd), nil
}
