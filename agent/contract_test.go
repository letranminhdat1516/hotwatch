package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"hotwatch/collect"
	"hotwatch/docker"
	"hotwatch/model"
)

const tok = "s3cret-token"

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type rig struct {
	srv  *httptest.Server
	eng  *docker.FakeEngineAPI
	sig  *FakeSignaller
	log  *syncBuf
	now  *time.Time
	nowM sync.Mutex
}

func newRig(t *testing.T, readOnly bool) *rig {
	t.Helper()
	r := &rig{eng: &docker.FakeEngineAPI{Containers: []model.Container{{ID: "jobhunter-bot", Name: "jobhunter-bot"}}}, sig: &FakeSignaller{}, log: &syncBuf{}}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.now = &start
	cfg := Config{TokenSHA256: SHA256Hex(tok), ReadOnly: readOnly, Now: func() time.Time { r.nowM.Lock(); defer r.nowM.Unlock(); return *r.now }}
	r.srv = httptest.NewServer(New(cfg, &collect.Fake{}, r.eng, r.sig, r.log))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *rig) advance(d time.Duration) { r.nowM.Lock(); *r.now = r.now.Add(d); r.nowM.Unlock() }

func do(t *testing.T, srv *httptest.Server, method, path, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func apiCode(t *testing.T, res *http.Response) string {
	t.Helper()
	var e model.APIError
	if err := json.NewDecoder(res.Body).Decode(&e); err != nil {
		t.Fatalf("decode APIError: %v", err)
	}
	return e.Code
}

func TestAuthAndReadOnly(t *testing.T) {
	r := newRig(t, true)
	if res := do(t, r.srv, "GET", "/v1/info", "", ""); res.StatusCode != 401 || apiCode(t, res) != "unauthorized" {
		t.Fatalf("no token: %d", res.StatusCode)
	}
	if res := do(t, r.srv, "GET", "/v1/info", "wrong", ""); res.StatusCode != 401 {
		t.Fatalf("wrong token: %d", res.StatusCode)
	}
	res := do(t, r.srv, "POST", "/v1/containers/jobhunter-bot/restart", tok, "")
	if res.StatusCode != 403 || apiCode(t, res) != "forbidden_read_only" {
		t.Fatalf("read-only restart: %d", res.StatusCode)
	}
	// every POST route is refused, even with bad ids, before any work
	for _, p := range []string{"/v1/containers/jobhunter-bot/start", "/v1/containers/jobhunter-bot/stop", "/v1/containers/nope/restart", "/v1/processes/4242/signal", "/v1/processes/abc/signal"} {
		res := do(t, r.srv, "POST", p, tok, `{"signal":"TERM","startTime":"2026-01-01T00:00:00Z"}`)
		if res.StatusCode != 403 {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
	if len(r.eng.Done) != 0 || len(r.sig.Calls()) != 0 {
		t.Fatal("read-only did work")
	}
	if res := do(t, r.srv, "GET", "/v1/info", tok, ""); res.StatusCode != 200 {
		t.Fatalf("GET still allowed: %d", res.StatusCode)
	}
}

func TestLockout(t *testing.T) {
	r := newRig(t, false)
	for i := 0; i < 5; i++ {
		if res := do(t, r.srv, "GET", "/v1/info", "bad", ""); res.StatusCode != 401 {
			t.Fatalf("attempt %d: %d", i, res.StatusCode)
		}
	}
	// invalid attempts are throttled while locked...
	res := do(t, r.srv, "GET", "/v1/info", "bad", "")
	if res.StatusCode != 429 || apiCode(t, res) != "too_many_requests" {
		t.Fatalf("bad token while locked: %d", res.StatusCode)
	}
	if res := do(t, r.srv, "GET", "/v1/info", "", ""); res.StatusCode != 429 {
		t.Fatalf("no token while locked: %d", res.StatusCode)
	}
	// ...but the valid token is never denied (shared 127.0.0.1 over SSH tunnels)
	if res := do(t, r.srv, "GET", "/v1/info", tok, ""); res.StatusCode != 200 {
		t.Fatalf("valid token while locked: %d", res.StatusCode)
	}
	// a success does not clear the counter
	if res := do(t, r.srv, "GET", "/v1/info", "bad", ""); res.StatusCode != 429 {
		t.Fatalf("success cleared lockout: %d", res.StatusCode)
	}
	r.advance(59 * time.Second)
	if res := do(t, r.srv, "GET", "/v1/info", "bad", ""); res.StatusCode != 429 {
		t.Fatalf("still locked: %d", res.StatusCode)
	}
	r.advance(2 * time.Second)
	if res := do(t, r.srv, "GET", "/v1/info", "bad", ""); res.StatusCode != 401 {
		t.Fatalf("after lockout: %d", res.StatusCode)
	}
}

func TestEmptyConfigOrTokenRejected(t *testing.T) {
	for _, digest := range []string{"", SHA256Hex("")} {
		srv := httptest.NewServer(New(Config{TokenSHA256: digest}, &collect.Fake{}, &docker.FakeEngineAPI{}, &FakeSignaller{}, io.Discard))
		defer srv.Close()
		for _, tk := range []string{"", tok} {
			if res := do(t, srv, "GET", "/v1/info", tk, ""); res.StatusCode != 401 {
				t.Errorf("digest %q token %q: %d", digest, tk, res.StatusCode)
			}
		}
		req, _ := http.NewRequest("GET", srv.URL+"/v1/info", nil)
		req.Header.Set("Authorization", "Bearer ")
		if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 {
			t.Errorf("empty bearer with digest %q: %d", digest, res.StatusCode)
		}
	}
}

func TestFailuresOutsideWindowDoNotLock(t *testing.T) {
	r := newRig(t, false)
	for i := 0; i < 4; i++ {
		do(t, r.srv, "GET", "/v1/info", "bad", "")
	}
	r.advance(61 * time.Second)
	do(t, r.srv, "GET", "/v1/info", "bad", "")
	if res := do(t, r.srv, "GET", "/v1/info", tok, ""); res.StatusCode != 200 {
		t.Fatalf("old failures counted: %d", res.StatusCode)
	}
}

func TestInputValidation(t *testing.T) {
	r := newRig(t, false)
	sig := `{"signal":"TERM","startTime":"2026-01-01T00:00:00Z"}`
	cases := []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"POST", "/v1/containers/nope/restart", "", 404, "not_found"},
		{"POST", "/v1/containers/-bad/restart", "", 400, "bad_request"},
		{"POST", "/v1/containers/a%2Fb/restart", "", 400, "bad_request"},
		{"POST", "/v1/containers/" + strings.Repeat("a", 129) + "/restart", "", 400, "bad_request"},
		{"POST", "/v1/containers/jobhunter-bot/explode", "", 404, "not_found"},
		{"GET", "/v1/containers/nope/logs", "", 404, "not_found"},
		{"POST", "/v1/processes/1/signal", sig, 400, "bad_request"},
		{"POST", "/v1/processes/0/signal", sig, 400, "bad_request"},
		{"POST", "/v1/processes/-5/signal", sig, 400, "bad_request"},
		{"POST", "/v1/processes/abc/signal", sig, 400, "bad_request"},
		{"POST", "/v1/processes/99999999999/signal", sig, 400, "bad_request"},
		{"POST", "/v1/processes/4242/signal", `{"signal":"HUP","startTime":"2026-01-01T00:00:00Z"}`, 400, "bad_request"},
		{"POST", "/v1/processes/4242/signal", `{"signal":"TERM"}`, 400, "bad_request"},
		{"POST", "/v1/processes/4242/signal", `{"signal":"TERM","startTime":"yesterday"}`, 400, "bad_request"},
		{"POST", "/v1/processes/4242/signal", `not json`, 400, "bad_request"},
		{"POST", "/v1/processes/4242/signal", `{"signal":"TERM","startTime":"2026-01-01T00:00:00Z","pad":"` + strings.Repeat("x", 5000) + `"}`, 400, "bad_request"},
		{"POST", "/v1/processes/4242/signal", `{"signal":"TERM","startTime":"2026-01-01T00:00:00Z","extra":1}`, 400, "bad_request"},
		{"POST", "/v1/processes/4242/signal", `{"signal":"TERM","startTime":"2026-01-01T00:00:00Z"} {"signal":"KILL"}`, 400, "bad_request"},
		{"GET", "/v1/nothing", "", 404, "not_found"},
	}
	for _, c := range cases {
		res := do(t, r.srv, c.method, c.path, tok, c.body)
		if res.StatusCode != c.status || apiCode(t, res) != c.code {
			t.Errorf("%s %s: status %d", c.method, c.path, res.StatusCode)
		}
	}
	if len(r.eng.Done) != 0 || len(r.sig.Calls()) != 0 {
		t.Fatal("invalid input reached the engine/signaller")
	}
}

func TestActionsAndSignalReachBackends(t *testing.T) {
	r := newRig(t, false)
	if res := do(t, r.srv, "POST", "/v1/containers/jobhunter-bot/restart", tok, ""); res.StatusCode != 204 {
		t.Fatalf("restart: %d", res.StatusCode)
	}
	if len(r.eng.Done) != 1 || r.eng.Done[0] != "jobhunter-bot:restart" {
		t.Fatalf("done %v", r.eng.Done)
	}
	start := time.Date(2026, 1, 1, 1, 2, 3, 456, time.UTC)
	body := `{"signal":"KILL","startTime":"` + start.Format(time.RFC3339Nano) + `"}`
	if res := do(t, r.srv, "POST", "/v1/processes/4242/signal", tok, body); res.StatusCode != 204 {
		t.Fatalf("signal: %d", res.StatusCode)
	}
	calls := r.sig.Calls()
	if len(calls) != 1 || calls[0].PID != 4242 || !calls[0].Start.Equal(start) || calls[0].Sig != model.SigKill {
		t.Fatalf("calls %+v", calls)
	}
}

func TestSignallerErrorPassthrough(t *testing.T) {
	for _, c := range []struct {
		code   string
		status int
	}{{"conflict", 409}, {"forbidden", 403}, {"not_found", 404}} {
		r := newRig(t, false)
		r.sig.Err = &model.APIError{Code: c.code, Message: "boom"}
		res := do(t, r.srv, "POST", "/v1/processes/4242/signal", tok, `{"signal":"TERM","startTime":"2026-01-01T00:00:00Z"}`)
		if res.StatusCode != c.status || apiCode(t, res) != c.code {
			t.Errorf("%s: %d", c.code, res.StatusCode)
		}
	}
	r := newRig(t, false)
	r.sig.Err = io.ErrUnexpectedEOF
	if res := do(t, r.srv, "POST", "/v1/processes/4242/signal", tok, `{"signal":"TERM","startTime":"2026-01-01T00:00:00Z"}`); res.StatusCode != 500 {
		t.Errorf("plain error: %d", res.StatusCode)
	}
}

func TestDockerFailureIs502(t *testing.T) {
	r := newRig(t, false)
	r.eng.Err = io.ErrClosedPipe
	res := do(t, r.srv, "GET", "/v1/containers", tok, "")
	if res.StatusCode != 502 || apiCode(t, res) != "docker_unavailable" {
		t.Fatalf("got %d", res.StatusCode)
	}
}

func TestLogsStreamAndTailClamp(t *testing.T) {
	r := newRig(t, false)
	now := time.Now().UTC()
	r.eng.Lines = []model.LogLine{{Time: now, Stream: "stdout", Text: "one"}, {Time: now, Stream: "stderr", Text: "two"}}
	res := do(t, r.srv, "GET", "/v1/containers/jobhunter-bot/logs?tail=999999&follow=0", tok, "")
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), "ndjson") {
		t.Fatalf("logs: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	dec := json.NewDecoder(res.Body)
	var got []string
	for dec.More() {
		var l model.LogLine
		if err := dec.Decode(&l); err != nil {
			t.Fatal(err)
		}
		got = append(got, l.Text)
	}
	if strings.Join(got, ",") != "one,two" {
		t.Fatalf("lines %v", got)
	}
}

func TestLogsStopOnDisconnect(t *testing.T) {
	r := newRig(t, false)
	r.eng.Lines = []model.LogLine{{Text: "one"}}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/v1/containers/jobhunter-bot/logs?follow=1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var l model.LogLine
	if err := json.NewDecoder(res.Body).Decode(&l); err != nil || l.Text != "one" {
		t.Fatalf("first line: %v %+v", err, l)
	}
	cancel()
	res.Body.Close()
	// httptest.Server.Close blocks until every handler has returned.
	done := make(chan struct{})
	go func() { r.srv.Close(); close(done) }() // Close blocks until handlers return
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("logs handler still running after client disconnect")
	}
}

func TestAudit(t *testing.T) {
	r := newRig(t, false)
	do(t, r.srv, "POST", "/v1/containers/jobhunter-bot/restart", tok, "")
	do(t, r.srv, "GET", "/v1/info", tok, "")                 // GET success: not audited
	do(t, r.srv, "GET", "/v1/info", "leaky-wrong-token", "") // failed auth: audited
	lines := strings.Split(strings.TrimSpace(r.log.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 audit lines, got %d: %q", len(lines), r.log.String())
	}
	var e map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"time", "remote", "method", "route", "target", "result"} {
		if _, ok := e[k]; !ok {
			t.Errorf("missing %q in %v", k, e)
		}
	}
	if e["method"] != "POST" || e["result"] != float64(204) || !strings.Contains(e["target"].(string), "jobhunter-bot") || !strings.Contains(e["remote"].(string), "127.0.0.1") {
		t.Errorf("entry %v", e)
	}
	if e["signal"] != nil {
		t.Errorf("unexpected signal on container action: %v", e)
	}
	var f map[string]any
	json.Unmarshal([]byte(lines[1]), &f)
	if f["result"] != float64(401) {
		t.Errorf("failed auth entry %v", f)
	}
	out := r.log.String()
	if strings.Contains(out, tok) || strings.Contains(out, "leaky-wrong-token") || strings.Contains(strings.ToLower(out), "bearer") || strings.Contains(strings.ToLower(out), "authorization") {
		t.Fatalf("audit leaks credentials: %s", out)
	}
}

func TestAuditRecordsSignal(t *testing.T) {
	r := newRig(t, false)
	do(t, r.srv, "POST", "/v1/processes/4242/signal", tok, `{"signal":"KILL","startTime":"2026-01-01T00:00:00Z"}`)
	if !strings.Contains(r.log.String(), `"signal":"KILL"`) {
		t.Fatalf("audit: %s", r.log.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }

func TestAuditWriteFailureReported(t *testing.T) {
	var out syncBuf
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	srv := httptest.NewServer(New(Config{TokenSHA256: SHA256Hex(tok)}, &collect.Fake{}, &docker.FakeEngineAPI{Containers: []model.Container{{ID: "web"}}}, &FakeSignaller{}, failWriter{}))
	defer srv.Close()
	if res := do(t, srv, "POST", "/v1/containers/web/start", tok, ""); res.StatusCode != 204 {
		t.Fatalf("action must still proceed: %d", res.StatusCode)
	}
	if !strings.Contains(out.String(), "audit write failed") {
		t.Fatalf("stderr log: %q", out.String())
	}
}

// slowEngine delays Do and records whether its context was cancelled.
type slowEngine struct {
	*docker.FakeEngineAPI
	delay     time.Duration
	started   chan struct{}
	ctxErr    chan error
	deadlined chan bool
}

func (e *slowEngine) Do(ctx context.Context, id string, act model.ContainerAction) error {
	_, has := ctx.Deadline()
	e.deadlined <- has
	close(e.started)
	select {
	case <-time.After(e.delay):
	case <-ctx.Done():
	}
	e.ctxErr <- ctx.Err()
	return e.FakeEngineAPI.Do(context.Background(), id, act)
}

func TestActionSurvivesClientDisconnect(t *testing.T) {
	eng := &slowEngine{FakeEngineAPI: &docker.FakeEngineAPI{Containers: []model.Container{{ID: "web"}}}, delay: 300 * time.Millisecond,
		started: make(chan struct{}), ctxErr: make(chan error, 1), deadlined: make(chan bool, 1)}
	srv := httptest.NewServer(New(Config{TokenSHA256: SHA256Hex(tok), ActionTimeout: 5 * time.Second}, &collect.Fake{}, eng, &FakeSignaller{}, io.Discard))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/v1/containers/web/restart", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	go http.DefaultClient.Do(req)
	<-eng.started
	cancel() // client gives up mid-action
	if err := <-eng.ctxErr; err != nil {
		t.Fatalf("action context cancelled by client disconnect: %v", err)
	}
	if !<-eng.deadlined {
		t.Fatal("action context has no timeout")
	}
	eng.FakeEngineAPI.Mu.Lock()
	defer eng.FakeEngineAPI.Mu.Unlock()
	if len(eng.FakeEngineAPI.Done) != 1 {
		t.Fatalf("done %v", eng.FakeEngineAPI.Done)
	}
}
