package collect

import (
	"context"
	"sync"

	"hotwatch/model"
)

// Fake is a scriptable Collector for tests.
type Fake struct {
	Mu    sync.Mutex
	Snap  model.Snapshot
	Procs []model.Process
	Err   error
}

var _ Collector = (*Fake)(nil)

func (f *Fake) Sample(ctx context.Context) (model.Snapshot, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return model.Snapshot{}, f.Err
	}
	return f.Snap, ctx.Err()
}

func (f *Fake) Processes(ctx context.Context) ([]model.Process, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]model.Process(nil), f.Procs...), ctx.Err()
}
