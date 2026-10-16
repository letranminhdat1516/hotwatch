//go:build !windows

package main

import (
	"context"
	"net"

	"hotwatch/docker"
)

const dockerSocket = "/var/run/docker.sock"

func auditPath() string { return "/var/log/hotwatch/audit.log" }

// prepareDataDir is a no-op: the installer creates /var/log/hotwatch for
// the service user.
func prepareDataDir() error { return nil }

func dockerEngine() (docker.Engine, func(ctx context.Context) (net.Conn, error)) {
	dial := func(ctx context.Context) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", dockerSocket)
	}
	return docker.NewClientDial(dial), dial
}

// runService reports false: off Windows the agent runs under systemd (or
// another supervisor) as a plain process.
func runService(func(ctx context.Context) error) (bool, error) { return false, nil }
