package hub

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"hotwatch/model"
	"hotwatch/source"
)

const (
	actionTimeout = 70 * time.Second // agent action timeout is 60 s
	maxActionLog  = 500
)

// Executor performs a's work, reporting progress through log. The Action it
// receives is a private snapshot.
type Executor func(ctx context.Context, a *Action, log func(string)) error

// ActionRunner implements Runner on top of an Executor.
type ActionRunner struct {
	exec    Executor
	publish func(Action)

	mu      sync.Mutex
	next    ActionID
	actions map[ActionID]*Action
	busy    map[string]ActionID // HostID+Target -> running action
	cancels map[ActionID]context.CancelFunc
	stopped bool
	wg      sync.WaitGroup
}

var _ Runner = (*ActionRunner)(nil)

// NewRunner returns a runner executing actions with exec. publish receives a
// copy of the Action on every state change and log line, from runner
// goroutines.
func NewRunner(exec Executor, publish func(Action)) *ActionRunner {
	return &ActionRunner{
		exec: exec, publish: publish,
		actions: map[ActionID]*Action{},
		busy:    map[string]ActionID{},
		cancels: map[ActionID]context.CancelFunc{},
	}
}

func cloneAction(a *Action) Action {
	c := *a
	c.Log = append([]string(nil), a.Log...)
	if a.Err != nil {
		e := *a.Err
		c.Err = &e
	}
	return c
}

func apiErr(code, msg string) *model.APIError { return &model.APIError{Code: code, Message: msg} }

// toAPIError maps err to the *model.APIError carried by an Action.
func toAPIError(err error) *model.APIError {
	var v model.APIError
	if errors.As(err, &v) {
		return &v
	}
	var p *model.APIError
	if errors.As(err, &p) && p != nil {
		e := *p
		return &e
	}
	return apiErr("internal", err.Error())
}

// Start runs a, which the caller has already confirmed (State "confirming").
// A rejected or conflicting action is recorded as failed and not executed.
func (r *ActionRunner) Start(a Action) ActionID {
	r.mu.Lock()
	r.next++
	a.ID = r.next
	a.Log = nil
	a.Err = nil
	key := a.HostID + "\x00" + a.Target
	var reject *model.APIError
	switch {
	case r.stopped:
		reject = apiErr("cancelled", "runner stopped")
	case a.State != "confirming":
		reject = apiErr("bad_request", fmt.Sprintf("action must be confirming, not %q", a.State))
	case r.busy[key] != 0:
		reject = apiErr("conflict", "another action is running on this target")
	}
	if reject != nil {
		a.State, a.Err = "failed", reject
		r.actions[a.ID] = &a
		out := cloneAction(&a)
		r.mu.Unlock()
		r.publish(out)
		return a.ID
	}
	a.State = "running"
	r.actions[a.ID] = &a
	r.busy[key] = a.ID
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	r.cancels[a.ID] = cancel
	r.wg.Add(1)
	run, out := cloneAction(&a), cloneAction(&a)
	r.mu.Unlock()

	r.publish(out)
	go r.run(ctx, cancel, key, &run)
	return run.ID
}

func (r *ActionRunner) run(ctx context.Context, cancel context.CancelFunc, key string, snap *Action) {
	defer r.wg.Done()
	id := snap.ID
	log := func(line string) {
		r.mu.Lock()
		a := r.actions[id]
		a.Log = append(a.Log, line)
		if n := len(a.Log); n > maxActionLog {
			a.Log = append([]string(nil), a.Log[n-maxActionLog:]...)
		}
		out := cloneAction(a)
		r.mu.Unlock()
		r.publish(out)
	}
	err := r.exec(ctx, snap, log)
	cancel()

	r.mu.Lock()
	a := r.actions[id]
	switch {
	case err == nil:
		a.State = "succeeded"
	case r.stopped && ctx.Err() != nil:
		a.State = "cancelled"
	default:
		a.State, a.Err = "failed", toAPIError(err)
	}
	delete(r.busy, key)
	delete(r.cancels, id)
	out := cloneAction(a)
	r.mu.Unlock()
	r.publish(out)
}

// Get returns a copy of the action, or the zero Action if id is unknown.
func (r *ActionRunner) Get(id ActionID) Action {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.actions[id]; ok {
		return cloneAction(a)
	}
	return Action{}
}

// Stop cancels running actions (they end "cancelled"), waits for their
// goroutines, and rejects later Starts.
func (r *ActionRunner) Stop() {
	r.mu.Lock()
	r.stopped = true
	for _, c := range r.cancels {
		c()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// ComposeExecutors dispatches by Kind; unknown kinds fail.
func ComposeExecutors(m map[ActionKind]Executor) Executor {
	return func(ctx context.Context, a *Action, log func(string)) error {
		if e, ok := m[a.Kind]; ok && e != nil {
			return e(ctx, a, log)
		}
		return errors.New("unsupported action kind")
	}
}

// SourceExecutor runs "signal" and "container" actions on the host's Source.
func SourceExecutor(lookup func(hostID string) (source.Source, bool)) Executor {
	return func(ctx context.Context, a *Action, _ func(string)) error {
		switch a.Kind {
		case "signal":
			return execSignal(ctx, lookup, a)
		case "container":
			return execContainer(ctx, lookup, a)
		}
		return errors.New("unsupported action kind")
	}
}

func badRequest(format string, args ...any) error {
	return model.APIError{Code: "bad_request", Message: fmt.Sprintf(format, args...)}
}

func hostSource(lookup func(string) (source.Source, bool), id string) (source.Source, error) {
	s, ok := lookup(id)
	if !ok || s == nil {
		return nil, model.APIError{Code: "not_found", Message: "unknown host " + id}
	}
	return s, nil
}

func execSignal(ctx context.Context, lookup func(string) (source.Source, bool), a *Action) error {
	pidS, startS, ok := strings.Cut(a.Target, "@")
	if !ok {
		return badRequest("signal target %q is not <pid>@<start>", a.Target)
	}
	pid, err := strconv.ParseInt(pidS, 10, 32)
	if err != nil || pid <= 0 {
		return badRequest("bad pid %q", pidS)
	}
	ns, err := strconv.ParseInt(startS, 10, 64)
	if err != nil {
		return badRequest("bad start time %q", startS)
	}
	sig := model.Signal(a.Verb)
	if sig != model.SigTerm && sig != model.SigKill {
		return badRequest("unknown signal %q", a.Verb)
	}
	s, err := hostSource(lookup, a.HostID)
	if err != nil {
		return err
	}
	return s.Signal(ctx, int32(pid), time.Unix(0, ns), sig)
}

func execContainer(ctx context.Context, lookup func(string) (source.Source, bool), a *Action) error {
	act := model.ContainerAction(a.Verb)
	if act != model.ActionStart && act != model.ActionStop && act != model.ActionRestart {
		return badRequest("unknown container action %q", a.Verb)
	}
	if a.Target == "" {
		return badRequest("empty container id")
	}
	s, err := hostSource(lookup, a.HostID)
	if err != nil {
		return err
	}
	return s.ContainerAction(ctx, a.Target, act)
}
