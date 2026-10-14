package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hotwatch/agent"
	"hotwatch/collect/linux"
	"hotwatch/docker"
	"hotwatch/gpu/nvidia"
	"hotwatch/model"
)

const (
	auditPath    = "/var/log/hotwatch/audit.log"
	dockerSocket = "/var/run/docker.sock"
)

// newHTTPServer applies the agent's connection limits. Action and log routes
// extend their own write deadline.
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func openAudit(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640)
}

// unwiredSignaller refuses every signal until the process-signalling backend
// is wired in.
type unwiredSignaller struct{}

func (unwiredSignaller) Signal(int32, time.Time, model.Signal) error {
	return &model.APIError{Code: "forbidden", Message: "process signalling is not available in this build"}
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	path := fs.String("config", agent.DefaultConfigPath, "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	cfg, err := agent.LoadConfig(*path)
	if err != nil {
		return err
	}
	if err := checkAssigned(cfg.Listen, interfaceIPs); err != nil {
		return err
	}
	audit, err := openAudit(auditPath)
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	defer audit.Close()
	return serve(cfg, audit, unwiredSignaller{})
}

func serve(cfg agent.FileConfig, audit io.Writer, sig agent.Signaller) error {
	ln, err := net.Listen("tcp4", cfg.Listen)
	if err != nil {
		return err
	}
	probe := newDockerProbe(dockerSocket)
	h := agent.New(agent.Config{
		TokenSHA256: cfg.TokenSHA256,
		ReadOnly:    cfg.ReadOnly,
		Interval:    time.Duration(cfg.IntervalMs) * time.Millisecond,
		Info:        hostInfo(probe, hasNvidiaSMI),
		GPU: func(ctx context.Context) ([]model.GPU, error) {
			if !hasNvidiaSMI() {
				return nil, nil
			}
			return nvidia.Read(ctx, nvidia.Run)
		},
	}, linux.New("/"), docker.NewClient(dockerSocket), sig, audit)

	srv := newHTTPServer(h)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Printf("hotwatch-agent %s listening on %s (readOnly=%v)", version, cfg.Listen, cfg.ReadOnly)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		srv.Close()
	}
	return nil
}
