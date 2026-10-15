package agent

import (
	"context"
	"sync"
	"time"

	"hotwatch/model"
)

// SignalCall is one recorded FakeSignaller call.
type SignalCall struct {
	PID   int32
	Start time.Time
	Sig   model.Signal
}

// FakeSignaller records Signal calls and returns Err (if set) from each.
type FakeSignaller struct {
	Mu    sync.Mutex
	Err   error
	calls []SignalCall
}

var _ Signaller = (*FakeSignaller)(nil)

func (f *FakeSignaller) Signal(_ context.Context, pid int32, start time.Time, s model.Signal) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.calls = append(f.calls, SignalCall{pid, start, s})
	return f.Err
}

// Calls returns a copy of the recorded calls.
func (f *FakeSignaller) Calls() []SignalCall {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return append([]SignalCall(nil), f.calls...)
}
