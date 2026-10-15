package apps

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// Fake serves List and Sizes and records what the page asked for, so a test
// can prove sizes are requested only for visible rows.
type Fake struct {
	Mu        sync.Mutex
	List      []App
	Sizes     map[string]uint64
	Err       error // returned by every call while non-nil
	SizeCalls []string
	Removed   []string
}

// Apps returns a copy of List with sizes cleared.
func (f *Fake) Apps(context.Context) ([]App, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	out := slices.Clone(f.List)
	for i := range out {
		out[i].Size = 0
	}
	return out, nil
}

// Size answers from Sizes and records the call.
func (f *Fake) Size(_ context.Context, bundle string) (uint64, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.SizeCalls = append(f.SizeCalls, bundle)
	if f.Err != nil {
		return 0, f.Err
	}
	n, ok := f.Sizes[bundle]
	if !ok {
		return 0, fmt.Errorf("apps: no such bundle %q", bundle)
	}
	return n, nil
}

// Uninstall records the bundle and drops it from List.
func (f *Fake) Uninstall(_ context.Context, bundle string) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Removed = append(f.Removed, bundle)
	f.List = slices.DeleteFunc(f.List, func(a App) bool { return a.Bundle == bundle })
	return nil
}
