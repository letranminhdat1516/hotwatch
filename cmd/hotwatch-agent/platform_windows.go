package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"

	"hotwatch/agent"
	"hotwatch/docker"
)

// serviceName is the Windows service the installer creates.
const serviceName = "hotwatch-agent"

// dockerPipe is Docker Engine's and Docker Desktop's named pipe.
const dockerPipe = `\\.\pipe\docker_engine`

// dataDir holds the config, the audit log and agent.log; it is checked (or
// created locked) before the agent, running as SYSTEM, writes there.
func dataDir() string { return filepath.Dir(agent.DefaultConfigPath) }

func prepareDataDir() error { return agent.PrepareConfigDir(dataDir()) }

func auditPath() string { return filepath.Join(dataDir(), "audit.log") }

func dockerEngine() (docker.Engine, func(ctx context.Context) (net.Conn, error)) {
	dial := func(ctx context.Context) (net.Conn, error) { return dialDockerPipe(ctx, dockerPipe) }
	return docker.NewClientDial(dial), dial
}

// dialDockerPipe connects to the daemon's pipe and refuses a server that
// could be a squatter: any user may create a pipe of that name while Docker
// is not running. The daemon runs as SYSTEM (Docker Engine) or from
// Program Files (Docker Desktop's backend); winio connects anonymously, so
// the server cannot impersonate the agent either way.
func dialDockerPipe(ctx context.Context, path string) (net.Conn, error) {
	c, err := winio.DialPipeContext(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := trustPipeServer(c); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func trustPipeServer(c net.Conn) error {
	f, ok := c.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("docker pipe: no handle to verify the server")
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(f.Fd()), &pid); err != nil {
		return fmt.Errorf("docker pipe: server process: %w", err)
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return fmt.Errorf("docker pipe: server process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err == nil {
		u, err := tok.GetTokenUser()
		tok.Close()
		if err == nil && u.User.Sid.IsWellKnown(windows.WinLocalSystemSid) {
			return nil
		}
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return fmt.Errorf("docker pipe: server image: %w", err)
	}
	exe := windows.UTF16ToString(buf[:n])
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_ProgramFiles, windows.FOLDERID_ProgramFilesX64} {
		dir, err := windows.KnownFolderPath(id, 0)
		if err == nil && dir != "" && strings.HasPrefix(strings.ToLower(exe), strings.ToLower(dir)+`\`) {
			return nil
		}
	}
	return fmt.Errorf("docker pipe is served by %s (pid %d), not by Docker; ignoring it", exe, pid)
}

// runService runs run under the service manager when the process was
// started as a service, logging to agent.log in the data folder; it
// reports false otherwise.
func runService(run func(ctx context.Context) error) (bool, error) {
	if ok, err := svc.IsWindowsService(); err != nil || !ok {
		return false, err
	}
	if err := prepareDataDir(); err != nil {
		return true, err
	}
	if f, err := os.OpenFile(filepath.Join(dataDir(), "agent.log"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	h := &service{run: run}
	if err := svc.Run(serviceName, h); err != nil {
		return true, err
	}
	return true, h.err
}

type service struct {
	run func(ctx context.Context) error
	err error
}

func (s *service) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			cancel()
			s.err = err
			if err != nil {
				log.Printf("hotwatch-agent: %v", err)
				return true, 1 // a service-specific failure: the manager may restart it
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				s.err = <-done
				return false, 0
			}
		}
	}
}
