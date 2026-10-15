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
	actionTimeout  = 70 * time.Second // agent action timeout is 60 s
	packageTimeout = 10 * time.Minute // brew/mise upgrades download and build
	maxActionLog   = 500
)

// Executor performs a's work, reporting progress through log. The Action it
// receives is a private snapshot.
type Executor func(ctx context.Context, a *Action, log func(string)) error

// ActionRunner implements Runner on top of an Executor.
type ActionRunner struct {
	exec    Executor
	publish func(Action)

	// pubMu serialises mutate+snapshot+publish so snapshots leave in Seq
	// order. Lock order: pubMu, then mu. publish and exec run without mu.
	pubMu sync.Mutex

	mu      sync.Mutex
	next    ActionID
	actions map[ActionID]*Action
	done    map[ActionID]bool   // final state taken; late log lines are dropped
	busy    map[string]ActionID // HostID+Target -> running action
	cancels map[ActionID]context.CancelFunc
	stopped bool
	wg      sync.WaitGroup
}

var _ Runner = (*ActionRunner)(nil)

// NewRunner returns a runner executing actions with exec. publish receives a
// copy of the Action on every state change and log line, from runner
// goroutines, in Seq order per runner.
func NewRunner(exec Executor, publish func(Action)) *ActionRunner {
	return &ActionRunner{
		exec: exec, publish: publish,
		actions: map[ActionID]*Action{},
		done:    map[ActionID]bool{},
		busy:    map[string]ActionID{},
		cancels: map[ActionID]context.CancelFunc{},
	}
}

// shortTimeout is "70s" or "10m".
func shortTimeout(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

// timeoutFor is how long an action of kind may run before it is cancelled.
func timeoutFor(kind ActionKind) time.Duration {
	if kind == "package" {
		return packageTimeout
	}
	return actionTimeout
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

// change applies f to action id under pubMu+mu, bumps Seq, and publishes the
// snapshot. f returns false to make no change.
func (r *ActionRunner) change(id ActionID, f func(a *Action) bool) {
	r.pubMu.Lock()
	defer r.pubMu.Unlock()
	r.mu.Lock()
	a := r.actions[id]
	if !f(a) {
		r.mu.Unlock()
		return
	}
	a.Seq++
	out := cloneAction(a)
	r.mu.Unlock()
	r.publish(out)
}

type startedAction struct {
	ctx    context.Context
	cancel context.CancelFunc
	key    string
	snap   Action
}

// Start runs a, which the caller has already confirmed (State "confirming").
// A rejected or conflicting action is recorded as failed and not executed.
func (r *ActionRunner) Start(a Action) ActionID {
	st, ok := r.begin(a)
	if !ok {
		return st.snap.ID
	}
	go r.run(st)
	return st.snap.ID
}

// begin registers a and publishes its first snapshot. ok reports that the
// action is running and run must be started.
func (r *ActionRunner) begin(a Action) (st startedAction, ok bool) {
	r.pubMu.Lock()
	defer r.pubMu.Unlock()
	r.mu.Lock()
	r.next++
	a.ID = r.next
	a.Log, a.Err = nil, nil
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
		a.Seq = 1
		r.actions[a.ID] = &a
		r.done[a.ID] = true
		out := cloneAction(&a)
		r.mu.Unlock()
		r.publish(out)
		return startedAction{snap: out}, false
	}
	a.State = "running"
	a.Seq = 1
	r.actions[a.ID] = &a
	r.busy[key] = a.ID
	ctx, cancel := context.WithTimeout(context.Background(), timeoutFor(a.Kind))
	r.cancels[a.ID] = cancel
	r.wg.Add(1)
	out := cloneAction(&a)
	r.mu.Unlock()
	r.publish(out)
	return startedAction{ctx: ctx, cancel: cancel, key: key, snap: cloneAction(&a)}, true
}

func (r *ActionRunner) run(st startedAction) {
	id := st.snap.ID
	var final func(a *Action)
	defer r.wg.Done()
	defer func() {
		// Final state, resource release and publish happen here so a panic
		// in the executor cannot leak the conflict key.
		r.pubMu.Lock()
		defer r.pubMu.Unlock()
		r.mu.Lock()
		a := r.actions[id]
		final(a)
		a.Seq++
		r.done[id] = true
		delete(r.busy, st.key)
		delete(r.cancels, id)
		out := cloneAction(a)
		r.mu.Unlock()
		r.publish(out)
	}()
	defer st.cancel()

	log := func(line string) {
		r.change(id, func(a *Action) bool {
			if r.done[id] {
				return false
			}
			a.Log = append(a.Log, line)
			if n := len(a.Log); n > maxActionLog {
				a.Log = append([]string(nil), a.Log[n-maxActionLog:]...)
			}
			return true
		})
	}

	if ctxErr := st.ctx.Err(); ctxErr != nil {
		final = r.finisher(nil, ctxErr, true)
		return
	}
	var err error
	func() {
		defer func() {
			if v := recover(); v != nil {
				log(fmt.Sprint("executor panic: ", v))
				err = apiErr("internal", "executor panicked")
			}
		}()
		err = r.exec(st.ctx, &st.snap, log)
	}()
	final = r.finisher(err, st.ctx.Err(), false) // ctx.Err() is read before cancel runs
}

// finisher returns the function setting the final state. Called without
// locks; the returned function runs under mu.
func (r *ActionRunner) finisher(err, ctxErr error, skipped bool) func(a *Action) {
	return func(a *Action) {
		switch {
		case err == nil && !skipped:
			a.State = "succeeded"
		case skipped && errors.Is(ctxErr, context.Canceled), errors.Is(err, context.Canceled) && errors.Is(ctxErr, context.Canceled) && r.stopped:
			a.State = "cancelled"
		case errors.Is(ctxErr, context.DeadlineExceeded) && (skipped || errors.Is(err, context.DeadlineExceeded)):
			a.State, a.Err = "failed", apiErr("timeout", "action timed out after "+shortTimeout(timeoutFor(a.Kind)))
		default:
			a.State, a.Err = "failed", toAPIError(err)
		}
	}
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
	r.cancelAll()
	r.wg.Wait()
}

func (r *ActionRunner) cancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	for _, c := range r.cancels {
		c()
	}
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
