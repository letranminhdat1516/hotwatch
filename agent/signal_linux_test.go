//go:build linux

package agent

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"hotwatch/collect/linux"
	"hotwatch/model"
)

func realLookup(ctx context.Context, pid int32) (model.Process, bool, error) {
	st, ok, err := linux.ProcessStart("/", pid)
	return model.Process{PID: pid, StartTime: st}, ok, err
}

func startSleeper(t *testing.T) (*exec.Cmd, chan error, time.Time) {
	t.Helper()
	cmd := exec.Command("sleep", "1000")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill() })
	st, ok, err := linux.ProcessStart("/", int32(cmd.Process.Pid))
	if err != nil || !ok {
		t.Fatalf("ProcessStart: %v %v", ok, err)
	}
	return cmd, done, st
}

func TestRealSignalKillsMatchingChild(t *testing.T) {
	cmd, done, st := startSleeper(t)
	s := NewSystemSignaller(realLookup)
	if err := s.Signal(bg, int32(cmd.Process.Pid), st, model.SigTerm); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); err == nil || !ok || ws.Signal() != syscall.SIGTERM {
			t.Fatalf("exit: err=%v state=%v", err, cmd.ProcessState)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child still running after SIGTERM")
	}
}

func TestRealSignalWrongStartLeavesChildAlive(t *testing.T) {
	cmd, done, st := startSleeper(t)
	s := NewSystemSignaller(realLookup)
	err := s.Signal(bg, int32(cmd.Process.Pid), st.Add(-time.Second), model.SigKill)
	if errCode(err) != "conflict" {
		t.Fatalf("err=%v", err)
	}
	select {
	case <-done:
		t.Fatal("child was signalled")
	case <-time.After(200 * time.Millisecond):
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("child gone: %v", err)
	}
}

func TestPidfdFallbackToKill(t *testing.T) {
	start := time.Unix(100, 0)
	var killed int
	s := &signaller{
		lookup: func(context.Context, int32) (model.Process, bool, error) {
			return model.Process{PID: 4242, StartTime: start}, true, nil
		},
		kill: func(int, syscall.Signal) error { killed++; return nil },
		pin:  func(int) (pinned, error) { return nil, syscall.ENOSYS }, // only ENOSYS falls back
	}
	if err := s.Signal(bg, 4242, start, model.SigTerm); err != nil || killed != 1 {
		t.Fatalf("err=%v killed=%d", err, killed)
	}
}
