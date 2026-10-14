package agent

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"hotwatch/collect"
	"hotwatch/model"
)

// fakeLookup serves a lookup from collect.Fake processes.
func fakeLookup(f *collect.Fake) func(context.Context, int32) (model.Process, bool, error) {
	return func(ctx context.Context, pid int32) (model.Process, bool, error) {
		ps, err := f.Processes(ctx)
		if err != nil {
			return model.Process{}, false, err
		}
		for _, p := range ps {
			if p.PID == pid {
				return p, true, nil
			}
		}
		return model.Process{}, false, nil
	}
}

func errCode(err error) string {
	var ae *model.APIError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func TestSignalRefusesReusedPID(t *testing.T) {
	col := &collect.Fake{Procs: []model.Process{{PID: 4242, StartTime: time.Unix(200, 0)}}}
	var killed []int
	s := NewSignaller(fakeLookup(col), func(pid int, _ syscall.Signal) error { killed = append(killed, pid); return nil })
	err := s.Signal(4242, time.Unix(100, 0), model.SigTerm)
	if errCode(err) != "conflict" || len(killed) != 0 {
		t.Fatalf("err=%v killed=%v", err, killed)
	}
}

func TestSignalRefusesPID1AndSelf(t *testing.T) {
	s := NewSignaller(fakeLookup(&collect.Fake{}), func(int, syscall.Signal) error { t.Fatal("must not kill"); return nil })
	for _, pid := range []int32{-5, 0, 1, int32(os.Getpid()), int32(os.Getppid())} {
		if err := s.Signal(pid, time.Now(), model.SigKill); errCode(err) != "forbidden" {
			t.Fatalf("pid %d: err=%v", pid, err)
		}
	}
}

func TestSignalDelivers(t *testing.T) {
	start := time.Unix(100, 5_000_000)
	col := &collect.Fake{Procs: []model.Process{{PID: 4242, StartTime: start}}}
	var gotPID int
	var gotSig syscall.Signal
	s := NewSignaller(fakeLookup(col), func(pid int, sig syscall.Signal) error { gotPID, gotSig = pid, sig; return nil })
	for sig, want := range map[model.Signal]syscall.Signal{model.SigTerm: syscall.SIGTERM, model.SigKill: syscall.SIGKILL} {
		gotPID, gotSig = 0, 0
		if err := s.Signal(4242, start, sig); err != nil || gotPID != 4242 || gotSig != want {
			t.Fatalf("%s: err=%v pid=%d sig=%v", sig, err, gotPID, gotSig)
		}
	}
	// Start times are compared exactly: one tick off is a different process.
	if err := s.Signal(4242, start.Add(10*time.Millisecond), model.SigTerm); errCode(err) != "conflict" {
		t.Fatalf("err=%v", err)
	}
}

func TestSignalErrors(t *testing.T) {
	start := time.Unix(100, 0)
	col := &collect.Fake{Procs: []model.Process{{PID: 4242, StartTime: start}}}
	kill := func(err error) Signaller {
		return NewSignaller(fakeLookup(col), func(int, syscall.Signal) error { return err })
	}
	err := kill(syscall.EPERM).Signal(4242, start, model.SigTerm)
	var ae *model.APIError
	if !errors.As(err, &ae) || ae.Code != "forbidden" || ae.Message != "not permitted (process owned by another user)" {
		t.Fatalf("EPERM: %v", err)
	}
	if err := kill(syscall.ESRCH).Signal(4242, start, model.SigTerm); errCode(err) != "not_found" {
		t.Fatalf("ESRCH: %v", err)
	}
	if err := kill(errors.New("boom")).Signal(4242, start, model.SigTerm); errCode(err) != "internal" {
		t.Fatalf("other: %v", err)
	}
	if err := kill(nil).Signal(999, start, model.SigTerm); errCode(err) != "not_found" {
		t.Fatalf("gone: %v", err)
	}
	if err := kill(nil).Signal(4242, start, model.Signal("HUP")); errCode(err) != "bad_request" {
		t.Fatalf("bad signal: %v", err)
	}
	failing := NewSignaller(func(context.Context, int32) (model.Process, bool, error) {
		return model.Process{}, false, errors.New("proc unreadable")
	}, func(int, syscall.Signal) error { t.Fatal("must not kill"); return nil })
	if err := failing.Signal(4242, start, model.SigTerm); errCode(err) != "internal" {
		t.Fatalf("lookup error: %v", err)
	}
}
