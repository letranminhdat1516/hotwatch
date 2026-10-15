package pages

import "hotwatch/hub"

// Actions is the pages' view of the actions the hub runs: the latest snapshot
// of each, by ID, and the one Start sent. The runner publishes snapshots from
// its own goroutines, possibly out of order by the time they reach the main
// thread, so Apply keeps the highest Seq per ID. Main thread only.
type Actions struct {
	runner hub.Runner
	byID   map[hub.ActionID]hub.Action
}

// NewActions returns an Actions starting actions on r.
func NewActions(r hub.Runner) *Actions {
	return &Actions{runner: r, byID: map[hub.ActionID]hub.Action{}}
}

// Apply records a published snapshot unless a newer one of the same action
// is already known.
func (s *Actions) Apply(a hub.Action) {
	if old, ok := s.byID[a.ID]; ok && old.Seq >= a.Seq {
		return
	}
	s.byID[a.ID] = a
}

// Start hands a (already confirmed by the user) to the runner. Until the
// runner publishes its first snapshot the action counts as running, so a
// second click cannot start a duplicate.
func (s *Actions) Start(a hub.Action) hub.ActionID {
	id := s.runner.Start(a)
	a.ID, a.State, a.Seq = id, "running", 0
	s.Apply(a)
	return id
}

// Running reports whether an action is running on target of hostID.
func (s *Actions) Running(hostID, target string) bool {
	for _, a := range s.byID {
		if a.HostID == hostID && a.Target == target && a.State == "running" {
			return true
		}
	}
	return false
}

// Last returns the most recently started action of kind on hostID.
func (s *Actions) Last(hostID string, kind hub.ActionKind) (hub.Action, bool) {
	var last hub.Action
	found := false
	for _, a := range s.byID {
		if a.HostID == hostID && a.Kind == kind && (!found || a.ID > last.ID) {
			last, found = a, true
		}
	}
	return last, found
}

// LastOn returns the most recently started action of kind on target of
// hostID.
func (s *Actions) LastOn(hostID string, kind hub.ActionKind, target string) (hub.Action, bool) {
	var last hub.Action
	found := false
	for _, a := range s.byID {
		if a.HostID == hostID && a.Kind == kind && a.Target == target && (!found || a.ID > last.ID) {
			last, found = a, true
		}
	}
	return last, found
}
