package agentclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hotwatch/agent"
	"hotwatch/collect"
	"hotwatch/docker"
	"hotwatch/model"
	"hotwatch/source"
)

var _ source.Source = (*Client)(nil)

const tok = "client-token"

func dialTo(srv *httptest.Server) func(context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", srv.Listener.Addr().String())
	}
}

type env struct {
	srv *httptest.Server
	c   *Client
	eng *docker.FakeEngineAPI
	sig *agent.FakeSignaller
	col *collect.Fake
}

func newEnv(t *testing.T, readOnly bool) *env {
	t.Helper()
	e := &env{eng: &docker.FakeEngineAPI{Containers: []model.Container{{ID: "web", Name: "web"}}}, sig: &agent.FakeSignaller{}, col: &collect.Fake{}}
	cfg := agent.Config{TokenSHA256: agent.SHA256Hex(tok), ReadOnly: readOnly, Info: func() model.HostInfo { return model.HostInfo{Hostname: "box", CPUCount: 8} }}
	e.srv = httptest.NewServer(agent.New(cfg, e.col, e.eng, e.sig, io.Discard))
	t.Cleanup(e.srv.Close)
	e.c = New(dialTo(e.srv), tok)
	return e
}

func TestRoundTrip(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	e.col.Snap = model.Snapshot{Time: time.Unix(100, 0).UTC()}
	e.col.Procs = []model.Process{{PID: 7, Name: "x"}}

	hi, err := e.c.Info(ctx)
	if err != nil || hi.Hostname != "box" || hi.ProtocolVersion != model.ProtocolVersion || hi.CPUCount != 8 {
		t.Fatalf("info %+v %v", hi, err)
	}
	snap, err := e.c.Snapshot(ctx)
	if err != nil || !snap.Time.Equal(time.Unix(100, 0)) {
		t.Fatalf("snapshot %+v %v", snap, err)
	}
	ps, err := e.c.Processes(ctx)
	if err != nil || len(ps) != 1 || ps[0].PID != 7 {
		t.Fatalf("procs %+v %v", ps, err)
	}
	cs, err := e.c.Containers(ctx)
	if err != nil || len(cs) != 1 {
		t.Fatalf("containers %+v %v", cs, err)
	}
	if err := e.c.ContainerAction(ctx, "web", model.ActionRestart); err != nil {
		t.Fatal(err)
	}
	if len(e.eng.Done) != 1 || e.eng.Done[0] != "web:restart" {
		t.Fatalf("done %v", e.eng.Done)
	}
	start := time.Date(2026, 2, 3, 4, 5, 6, 789, time.UTC)
	if err := e.c.Signal(ctx, 4242, start, model.SigTerm); err != nil {
		t.Fatal(err)
	}
	calls := e.sig.Calls()
	if len(calls) != 1 || calls[0].PID != 4242 || !calls[0].Start.Equal(start) || calls[0].Sig != model.SigTerm {
		t.Fatalf("calls %+v", calls)
	}
}

func apiErr(t *testing.T, err error) *model.APIError {
	t.Helper()
	var ae *model.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *APIError, got %T %v", err, err)
	}
	return ae
}

func TestErrorsAreAPIErrors(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()

	bad := New(dialTo(e.srv), "wrong")
	if _, err := bad.Info(ctx); apiErr(t, err).Code != "unauthorized" {
		t.Fatalf("401: %v", err)
	}
	if err := e.c.ContainerAction(ctx, "web", model.ActionStop); apiErr(t, err).Code != "forbidden_read_only" {
		t.Fatalf("read-only: %v", err)
	}
	e2 := newEnv(t, false)
	e2.sig.Err = &model.APIError{Code: "conflict", Message: "process start time changed"}
	err := e2.c.Signal(ctx, 4242, time.Now(), model.SigKill)
	if ae := apiErr(t, err); ae.Code != "conflict" || !strings.Contains(ae.Message, "start time") {
		t.Fatalf("conflict: %v", err)
	}
	if err := e2.c.ContainerAction(ctx, "ghost", model.ActionStart); apiErr(t, err).Code != "not_found" {
		t.Fatalf("404: %v", err)
	}
}

func TestNonAPIErrorBodyStillReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<html>bad gateway</html>", 502)
	}))
	defer srv.Close()
	_, err := New(dialTo(srv), "t").Info(context.Background())
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("got %v", err)
	}
}

func TestClientRejectsOversized(t *testing.T) {
	big := func(n int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("["))
			w.Write(bytes.Repeat([]byte(`{"pid":1},`), n))
			w.Write([]byte(`{"pid":2}]`))
		})
	}
	srv := httptest.NewServer(big(1_000_000))
	defer srv.Close()
	c := New(dialTo(srv), "t")
	for name, f := range map[string]func() error{
		"processes":  func() error { _, err := c.Processes(context.Background()); return err },
		"containers": func() error { _, err := c.Containers(context.Background()); return err },
		"snapshot":   func() error { _, err := c.Snapshot(context.Background()); return err },
		"info":       func() error { _, err := c.Info(context.Background()); return err },
	} {
		if err := f(); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("%s: want size error, got %v", name, err)
		}
	}
	// just under the cap is fine
	srv2 := httptest.NewServer(big(100_000)) // ~1 MB of procs, below 8 MB
	defer srv2.Close()
	if _, err := New(dialTo(srv2), "t").Processes(context.Background()); err != nil {
		t.Fatalf("under cap: %v", err)
	}
}

func TestRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	c := New(dialTo(srv), "t")
	c.timeout = 100 * time.Millisecond
	start := time.Now()
	if _, err := c.Info(context.Background()); err == nil {
		t.Fatal("want timeout")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout not applied")
	}
}

func TestLogsStreamAndCancel(t *testing.T) {
	e := newEnv(t, false)
	e.eng.Lines = []model.LogLine{{Stream: "stdout", Text: "a"}, {Stream: "stderr", Text: "b"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := e.c.ContainerLogs(ctx, "web", 50, true)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i := 0; i < 2; i++ {
		select {
		case l := <-ch:
			got = append(got, l.Text)
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout after %v", got)
		}
	}
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("got %v", got)
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected extra line")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

func TestLogsEndOfStreamClosesChannel(t *testing.T) {
	e := newEnv(t, false)
	e.eng.Lines = []model.LogLine{{Text: "only"}}
	ch, err := e.c.ContainerLogs(context.Background(), "web", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for range ch {
		n++
	}
	if n != 1 {
		t.Fatalf("lines %d", n)
	}
}

func TestLogsErrorBeforeStream(t *testing.T) {
	e := newEnv(t, false)
	_, err := e.c.ContainerLogs(context.Background(), "ghost", 10, false)
	if apiErr(t, err).Code != "not_found" {
		t.Fatalf("got %v", err)
	}
}

type slowEngine struct {
	*docker.FakeEngineAPI
	delay time.Duration
}

func (e *slowEngine) Do(ctx context.Context, id string, act model.ContainerAction) error {
	time.Sleep(e.delay)
	return e.FakeEngineAPI.Do(ctx, id, act)
}

func TestActionOutlastsReadTimeout(t *testing.T) {
	eng := &slowEngine{FakeEngineAPI: &docker.FakeEngineAPI{Containers: []model.Container{{ID: "web"}}}, delay: 400 * time.Millisecond}
	srv := httptest.NewServer(agent.New(agent.Config{TokenSHA256: agent.SHA256Hex(tok)}, &collect.Fake{}, eng, &agent.FakeSignaller{}, io.Discard))
	defer srv.Close()
	c := New(dialTo(srv), tok)
	c.timeout = 100 * time.Millisecond
	if err := c.ContainerAction(context.Background(), "web", model.ActionRestart); err != nil {
		t.Fatalf("action must use the longer timeout: %v", err)
	}
	if len(eng.Done) != 1 {
		t.Fatalf("done %v", eng.Done)
	}
	// reads keep the short timeout
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(400 * time.Millisecond) }))
	defer slow.Close()
	c2 := New(dialTo(slow), tok)
	c2.timeout = 100 * time.Millisecond
	if _, err := c2.Info(context.Background()); err == nil {
		t.Fatal("read should time out")
	}
}

func TestRedirectsNotFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/info" {
			t.Errorf("followed redirect to %s (credentials would be forwarded)", r.URL.Path)
		}
		http.Redirect(w, r, "/elsewhere", 302)
	}))
	defer srv.Close()
	if _, err := New(dialTo(srv), tok).Info(context.Background()); err == nil {
		t.Fatal("redirect must be an error")
	}
}
