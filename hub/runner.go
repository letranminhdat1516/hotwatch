// Package hub aggregates sources and runs confirmed actions.
package hub

import (
	"sync"

	"hotwatch/model"
)

type ActionID int64

type ActionKind string // "signal", "container", "package", "uninstall"

type ActionState string // "confirming","running","succeeded","failed","cancelled"

type Action struct {
	ID     ActionID
	Kind   ActionKind
	HostID string
	Target string
	Verb   string
	State  ActionState
	Err    *model.APIError
	Log    []string
}

// Runner starts actions and reports their progress.
type Runner interface {
	Start(a Action) ActionID
	Get(id ActionID) Action
}

// FakeRunner records started actions for tests.
type FakeRunner struct {
	mu      sync.Mutex
	Started []Action
	byID    map[ActionID]Action
}

var _ Runner = (*FakeRunner)(nil)

// Start stores a with a fresh sequential id (1, 2, ...).
func (r *FakeRunner) Start(a Action) ActionID {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID == nil {
		r.byID = map[ActionID]Action{}
	}
	a.ID = ActionID(len(r.Started) + 1)
	r.Started = append(r.Started, a)
	r.byID[a.ID] = a
	return a.ID
}

// Get returns the stored action, or the zero Action if id is unknown.
func (r *FakeRunner) Get(id ActionID) Action {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id]
}
