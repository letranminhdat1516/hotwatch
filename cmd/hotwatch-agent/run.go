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
	"hotwatch/gpu/nvidia"
	"hotwatch/model"
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

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func cmdRun(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runCtx(ctx, args)
}

// runCtx runs the agent until ctx ends: on SIGINT/SIGTERM from cmdRun, or
// when the Windows service manager stops the service.
func runCtx(ctx context.Context, args []string) error {
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
	if err := waitAssigned(cfg.Listen, interfaceIPs, wallClock{}, log.Printf); err != nil {
		return err
	}
	audit, err := openAudit(auditPath())
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	defer audit.Close()
	return serve(ctx, cfg, *path, audit, agent.NewSystemSignaller(procLookup))
}

func serve(ctx context.Context, cfg agent.FileConfig, cfgPath string, audit io.Writer, sig agent.Signaller) error {
	ln, err := net.Listen("tcp4", cfg.Listen)
	if err != nil {
		return err
	}
	engine, dial := dockerEngine()
	probe := newDockerProbe(dial)
	h := agent.New(agent.Config{
		Token:    agent.NewTokenReloader(cfgPath, cfg.TokenSHA256, nil, log.Printf).Digest,
		ReadOnly: cfg.ReadOnly,
		Interval: time.Duration(cfg.IntervalMs) * time.Millisecond,
		Info:     hostInfo(probe, hasNvidiaSMI),
		GPU: func(ctx context.Context) ([]model.GPU, error) {
			if !hasNvidiaSMI() {
				return nil, nil
			}
			return nvidia.Read(ctx, nvidia.Run)
		},
	}, newCollector(), engine, sig, audit)

	srv := newHTTPServer(h)
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
