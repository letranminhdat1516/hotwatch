package hub

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"hotwatch/model"
	"hotwatch/source"
)

type pubLog struct {
	mu  sync.Mutex
	got []Action
}

func (p *pubLog) publish(a Action) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, a)
}

func (p *pubLog) states() []ActionState {
	p.mu.Lock()
	defer p.mu.Unlock()
	var s []ActionState
	for _, a := range p.got {
		s = append(s, a.State)
	}
	return s
}

func lookupOf(s source.Source) func(string) (source.Source, bool) {
	return func(id string) (source.Source, bool) {
		if id == "h1" {
			return s, true
		}
		return nil, false
	}
}

func TestRunnerContainerRestartSucceeds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &source.Fake{}
		p := &pubLog{}
		r := NewRunner(SourceExecutor(lookupOf(f)), p.publish)
		id := r.Start(Action{Kind: "container", HostID: "h1", Target: "jobhunter-bot", Verb: "restart", State: "confirming"})
		synctest.Wait()
		got := r.Get(id)
		if got.State != "succeeded" || got.Err != nil {
			t.Fatalf("got %+v", got)
		}
		if len(f.Actions) != 1 || f.Actions[0] != "jobhunter-bot:restart" {
			t.Fatalf("actions %v", f.Actions)
		}
		st := p.states()
		if len(st) != 2 || st[0] != "running" || st[1] != "succeeded" {
			t.Fatalf("published %v", st)
		}
	})
}

func TestRunnerSourceErrorFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &source.Fake{Err: model.APIError{Code: "docker_unavailable", Message: "no docker"}}
		r := NewRunner(SourceExecutor(lookupOf(f)), func(Action) {})
		id := r.Start(Action{Kind: "container", HostID: "h1", Target: "c", Verb: "stop", State: "confirming"})
		synctest.Wait()
		got := r.Get(id)
		if got.State != "failed" || got.Err == nil || got.Err.Code != "docker_unavailable" || got.Err.Message != "no docker" {
			t.Fatalf("got %+v", got)
		}
		g := &source.Fake{Err: errors.New("boom")}
		r2 := NewRunner(SourceExecutor(lookupOf(g)), func(Action) {})
		id = r2.Start(Action{Kind: "container", HostID: "h1", Target: "c", Verb: "stop", State: "confirming"})
		synctest.Wait()
		if e := r2.Get(id).Err; e == nil || e.Code != "internal" || e.Message != "boom" {
			t.Fatalf("err %+v", e)
		}
	})
}

func TestRunnerLogLinesInOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &pubLog{}
		exec := func(_ context.Context, _ *Action, log func(string)) error {
			log("one")
			log("two")
			log("three")
			return nil
		}
		r := NewRunner(exec, p.publish)
		id := r.Start(Action{Kind: "package", HostID: "h1", Target: "wget", Verb: "upgrade", State: "confirming"})
		synctest.Wait()
		got := r.Get(id)
		if fmt.Sprint(got.Log) != "[one two three]" || got.State != "succeeded" {
			t.Fatalf("got %+v", got)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if n := len(p.got[3].Log); n != 3 { // running, 3 log lines, succeeded
			t.Fatalf("published log len %d", n)
		}
	})
}

func TestRunnerConflictSameTarget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		calls := 0
		exec := func(ctx context.Context, _ *Action, _ func(string)) error {
			calls++
			<-gate
			return nil
		}
		r := NewRunner(exec, func(Action) {})
		a := r.Start(Action{Kind: "container", HostID: "h1", Target: "c", Verb: "stop", State: "confirming"})
		synctest.Wait()
		b := r.Start(Action{Kind: "container", HostID: "h1", Target: "c", Verb: "stop", State: "confirming"})
		synctest.Wait()
		if got := r.Get(b); got.State != "failed" || got.Err == nil || got.Err.Code != "conflict" {
			t.Fatalf("second %+v", got)
		}
		if r.Get(a).State != "running" || calls != 1 {
			t.Fatalf("first %+v calls %d", r.Get(a), calls)
		}
		other := r.Start(Action{Kind: "container", HostID: "h2", Target: "c", Verb: "stop", State: "confirming"})
		synctest.Wait()
		if r.Get(other).State != "running" {
			t.Fatalf("other host blocked: %+v", r.Get(other))
		}
		close(gate)
		synctest.Wait()
		c := r.Start(Action{Kind: "container", HostID: "h1", Target: "c", Verb: "stop", State: "confirming"})
		synctest.Wait()
		if r.Get(c).State != "succeeded" {
			t.Fatalf("after release %+v", r.Get(c))
		}
	})
}

func TestRunnerRejectsNotConfirming(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		called := false
		exec := func(context.Context, *Action, func(string)) error { called = true; return nil }
		r := NewRunner(exec, func(Action) {})
		id := r.Start(Action{Kind: "container", HostID: "h1", Target: "c", Verb: "stop", State: "running"})
		synctest.Wait()
		got := r.Get(id)
		if got.State != "failed" || got.Err == nil || got.Err.Code != "bad_request" || called {
			t.Fatalf("got %+v called %v", got, called)
		}
	})
}

func TestRunnerTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		exec := func(ctx context.Context, _ *Action, _ func(string)) error {
			<-ctx.Done()
			return ctx.Err()
		}
		r := NewRunner(exec, func(Action) {})
		id := r.Start(Action{Kind: "container", HostID: "h1", Target: "x", State: "confirming"})
		time.Sleep(69 * time.Second)
		synctest.Wait()
		if r.Get(id).State != "running" {
			t.Fatalf("early: %+v", r.Get(id))
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if got := r.Get(id); got.State != "failed" || got.Err == nil || got.Err.Code != "timeout" || got.Err.Message != "action timed out after 70s" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestRunnerStopCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		exec := func(ctx context.Context, _ *Action, _ func(string)) error {
			<-ctx.Done()
			return ctx.Err()
		}
		r := NewRunner(exec, func(Action) {})
		id := r.Start(Action{Kind: "package", HostID: "h1", Target: "x", State: "confirming"})
		synctest.Wait()
		r.Stop()
		if got := r.Get(id); got.State != "cancelled" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestRunnerLogCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		exec := func(_ context.Context, _ *Action, log func(string)) error {
			for i := 0; i < 600; i++ {
				log(fmt.Sprint("l", i))
			}
			return nil
		}
		r := NewRunner(exec, func(Action) {})
		id := r.Start(Action{Kind: "package", HostID: "h1", Target: "x", State: "confirming"})
		synctest.Wait()
		got := r.Get(id)
		if len(got.Log) != 500 || got.Log[0] != "l100" || got.Log[499] != "l599" {
			t.Fatalf("len %d first %q last %q", len(got.Log), got.Log[0], got.Log[len(got.Log)-1])
		}
		got.Log[0] = "mutated"
		if r.Get(id).Log[0] != "l100" {
			t.Fatal("Get must return a copy")
		}
	})
}

func TestSourceExecutorSignalParsing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &source.Fake{}
		ex := SourceExecutor(lookupOf(f))
		a := &Action{Kind: "signal", HostID: "h1", Target: "42@1700000000000000000", Verb: "KILL"}
		if err := ex(context.Background(), a, func(string) {}); err != nil {
			t.Fatal(err)
		}
		if len(f.Signals) != 1 || f.Signals[0] != "42:KILL" {
			t.Fatalf("signals %v", f.Signals)
		}
		bad := []Action{
			{Kind: "signal", HostID: "h1", Target: "x@1", Verb: "TERM"},
			{Kind: "signal", HostID: "h1", Target: "42@x", Verb: "TERM"},
			{Kind: "signal", HostID: "h1", Target: "42", Verb: "TERM"},
			{Kind: "signal", HostID: "h1", Target: "42@1", Verb: "HUP"},
			{Kind: "container", HostID: "h1", Target: "c", Verb: "pause"},
			{Kind: "container", HostID: "h1", Target: "", Verb: "stop"},
			{Kind: "signal", HostID: "h1", Target: "42@1", Verb: ""},
		}
		for _, b := range bad {
			err := ex(context.Background(), &b, func(string) {})
			var ae model.APIError
			if !errors.As(err, &ae) || ae.Code != "bad_request" {
				t.Fatalf("%+v: err %v", b, err)
			}
		}
		if len(f.Signals) != 1 || len(f.Actions) != 0 {
			t.Fatalf("source called: %v %v", f.Signals, f.Actions)
		}
		err := ex(context.Background(), &Action{Kind: "package", HostID: "h1"}, func(string) {})
		if err == nil || err.Error() != "unsupported action kind" {
			t.Fatalf("err %v", err)
		}
		err = ex(context.Background(), &Action{Kind: "signal", HostID: "nope", Target: "1@1", Verb: "TERM"}, func(string) {})
		var ae model.APIError
		if !errors.As(err, &ae) || ae.Code != "not_found" {
			t.Fatalf("unknown host: %v", err)
		}
	})
}

func TestComposeExecutors(t *testing.T) {
	var hit string
	ex := ComposeExecutors(map[ActionKind]Executor{
		"package": func(context.Context, *Action, func(string)) error { hit = "package"; return nil },
	})
	if err := ex(context.Background(), &Action{Kind: "package"}, nil); err != nil || hit != "package" {
		t.Fatal(err, hit)
	}
	if err := ex(context.Background(), &Action{Kind: "uninstall"}, nil); err == nil || err.Error() != "unsupported action kind" {
		t.Fatal(err)
	}
}

func (p *pubLog) seqsAndLast(t *testing.T, id ActionID) Action {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	var prev uint64
	var last Action
	for _, a := range p.got {
		if a.ID != id {
			continue
		}
		if a.Seq <= prev {
			t.Fatalf("seq %d after %d", a.Seq, prev)
		}
		prev, last = a.Seq, a
	}
	return last
}

func TestRunnerPublishOrderedWithConcurrentAndLateLogs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := &pubLog{}
		var late func(string)
		exec := func(_ context.Context, _ *Action, log func(string)) error {
			late = log
			var wg sync.WaitGroup
			for g := 0; g < 2; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < 50; i++ {
						log("x")
					}
				}()
			}
			wg.Wait()
			return nil
		}
		r := NewRunner(exec, p.publish)
		id := r.Start(Action{Kind: "package", HostID: "h1", Target: "x", State: "confirming"})
		synctest.Wait()
		late("too late")
		synctest.Wait()
		last := p.seqsAndLast(t, id)
		if last.State != "succeeded" || len(last.Log) != 100 || r.Get(id).Seq != last.Seq {
			t.Fatalf("last %+v (log %d)", last.State, len(last.Log))
		}
	})
}

func TestRunnerPanicBecomesFailedAndReleasesTarget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := 0
		exec := func(context.Context, *Action, func(string)) error {
			n++
			if n == 1 {
				panic("kaboom")
			}
			return nil
		}
		p := &pubLog{}
		r := NewRunner(exec, p.publish)
		a := Action{Kind: "package", HostID: "h1", Target: "x", State: "confirming"}
		id := r.Start(a)
		synctest.Wait()
		got := r.Get(id)
		if got.State != "failed" || got.Err == nil || got.Err.Code != "internal" || got.Err.Message != "executor panicked" {
			t.Fatalf("got %+v", got)
		}
		if len(got.Log) != 1 || got.Log[0] != "executor panic: kaboom" {
			t.Fatalf("log %v", got.Log)
		}
		p.seqsAndLast(t, id)
		id2 := r.Start(a)
		synctest.Wait()
		if r.Get(id2).State != "succeeded" {
			t.Fatalf("key leaked: %+v", r.Get(id2))
		}
	})
}

func TestRunnerRealFailureAfterStopKeepsItsError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		exec := func(ctx context.Context, _ *Action, _ func(string)) error {
			<-ctx.Done()
			return model.APIError{Code: "forbidden", Message: "denied"}
		}
		r := NewRunner(exec, func(Action) {})
		id := r.Start(Action{Kind: "package", HostID: "h1", Target: "x", State: "confirming"})
		synctest.Wait()
		r.Stop()
		if got := r.Get(id); got.State != "failed" || got.Err == nil || got.Err.Code != "forbidden" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestRunnerCancelledBeforeExecNeverRunsExec(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		called := false
		exec := func(context.Context, *Action, func(string)) error { called = true; return nil }
		r := NewRunner(exec, func(Action) {})
		st, ok := r.begin(Action{Kind: "signal", HostID: "h1", Target: "1@1", Verb: "KILL", State: "confirming"})
		if !ok {
			t.Fatal("not started")
		}
		r.cancelAll() // Stop raced Start: ctx is done before run begins
		go r.run(st)
		r.Stop()
		if got := r.Get(st.snap.ID); got.State != "cancelled" || called {
			t.Fatalf("got %+v called %v", got, called)
		}
	})
}

func TestRunnerStartAfterStopFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		called := false
		r := NewRunner(func(context.Context, *Action, func(string)) error { called = true; return nil }, func(Action) {})
		r.Stop()
		id := r.Start(Action{Kind: "package", HostID: "h1", Target: "x", State: "confirming"})
		synctest.Wait()
		if got := r.Get(id); got.State != "failed" || got.Err == nil || got.Err.Code != "cancelled" || called {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestPackageTasksGetTenMinutes(t *testing.T) {
	if got := timeoutFor("package"); got != 10*time.Minute {
		t.Fatalf("package timeout %v, want 10m: brew upgrades take minutes", got)
	}
	if got := timeoutFor("container"); got != actionTimeout {
		t.Fatalf("container timeout %v, want %v", got, actionTimeout)
	}
}

func TestPackageTimeoutMessageNamesTenMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		exec := func(ctx context.Context, _ *Action, _ func(string)) error {
			<-ctx.Done()
			return ctx.Err()
		}
		r := NewRunner(exec, func(Action) {})
		id := r.Start(Action{Kind: "package", HostID: "h1", Target: "formula:go", State: "confirming"})
		time.Sleep(11 * time.Minute)
		synctest.Wait()
		if got := r.Get(id); got.Err == nil || got.Err.Message != "action timed out after 10m" {
			t.Fatalf("got %+v", got)
		}
	})
}
