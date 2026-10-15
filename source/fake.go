package source

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hotwatch/model"
)

// Fake is a scriptable Source for tests.
type Fake struct {
	Mu      sync.Mutex
	InfoV   model.HostInfo
	Snaps   []model.Snapshot // returned in order, last one repeats
	Procs   []model.Process
	Ctrs    []model.Container
	Err     error // returned by every call while non-nil
	Delay   time.Duration
	Signals []string // "pid:TERM" records
	Actions []string // "id:restart" records
	Logs    []model.LogLine

	next int
}

var _ Source = (*Fake)(nil)

// enter waits for Delay (honouring ctx) and returns the current error.
func (f *Fake) enter(ctx context.Context) error {
	f.Mu.Lock()
	d, err := f.Delay, f.Err
	f.Mu.Unlock()
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (f *Fake) Info(ctx context.Context) (model.HostInfo, error) {
	if err := f.enter(ctx); err != nil {
		return model.HostInfo{}, err
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return f.InfoV, nil
}

func (f *Fake) Snapshot(ctx context.Context) (model.Snapshot, error) {
	if err := f.enter(ctx); err != nil {
		return model.Snapshot{}, err
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if len(f.Snaps) == 0 {
		return model.Snapshot{}, nil
	}
	i := f.next
	if i >= len(f.Snaps) {
		i = len(f.Snaps) - 1
	} else {
		f.next++
	}
	return f.Snaps[i], nil
}

func (f *Fake) Processes(ctx context.Context) ([]model.Process, error) {
	if err := f.enter(ctx); err != nil {
		return nil, err
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return append([]model.Process(nil), f.Procs...), nil
}

func (f *Fake) Signal(ctx context.Context, pid int32, _ time.Time, sig model.Signal) error {
	if err := f.enter(ctx); err != nil {
		return err
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Signals = append(f.Signals, fmt.Sprintf("%d:%s", pid, sig))
	return nil
}

func (f *Fake) Containers(ctx context.Context) ([]model.Container, error) {
	if err := f.enter(ctx); err != nil {
		return nil, err
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return append([]model.Container(nil), f.Ctrs...), nil
}

func (f *Fake) ContainerAction(ctx context.Context, id string, act model.ContainerAction) error {
	if err := f.enter(ctx); err != nil {
		return err
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Actions = append(f.Actions, fmt.Sprintf("%s:%s", id, act))
	return nil
}

// ContainerLogs sends the configured Logs, then closes the channel.
func (f *Fake) ContainerLogs(ctx context.Context, _ string, _ int, _ bool) (<-chan model.LogLine, error) {
	if err := f.enter(ctx); err != nil {
		return nil, err
	}
	f.Mu.Lock()
	defer f.Mu.Unlock()
	ch := make(chan model.LogLine, len(f.Logs))
	for _, l := range f.Logs {
		ch <- l
	}
	close(ch)
	return ch, nil
}
