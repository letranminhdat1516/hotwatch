package main

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows/svc"

	"hotwatch/docker"
)

// serviceName is the Windows service the installer creates.
const serviceName = "hotwatch-agent"

// dockerPipe is Docker Desktop's and Docker Engine's named pipe.
const dockerPipe = `\\.\pipe\docker_engine`

func dataDir() string {
	dir := os.Getenv("ProgramData")
	if dir == "" {
		dir = `C:\ProgramData`
	}
	return filepath.Join(dir, "Hotwatch")
}

func auditPath() string { return filepath.Join(dataDir(), "audit.log") }

func dockerEngine() (docker.Engine, func(ctx context.Context) (net.Conn, error)) {
	dial := func(ctx context.Context) (net.Conn, error) { return winio.DialPipeContext(ctx, dockerPipe) }
	return docker.NewClientDial(dial), dial
}

// runService runs run under the service manager when the process was
// started as a service, logging to agent.log beside the config; it reports
// false otherwise.
func runService(run func(ctx context.Context) error) (bool, error) {
	if ok, err := svc.IsWindowsService(); err != nil || !ok {
		return false, err
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
