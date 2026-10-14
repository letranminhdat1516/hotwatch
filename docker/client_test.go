package docker

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hotwatch/model"
)

func seededID(f *FakeState) string {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	for k := range f.Containers {
		return k
	}
	return ""
}

func TestClientListParsesStatsAndInspect(t *testing.T) {
	sock, f := NewFakeEngine(t)
	id := seededID(f)
	started := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	f.Mu.Lock()
	f.Containers[id].StartedAt, f.Containers[id].Health = started, "healthy"
	f.Containers["exitedid1"] = &FakeContainer{ID: "exitedid1", Name: "old-job", Image: "busybox", State: "exited", Status: "Exited (0) 2 days ago"}
	f.Mu.Unlock()

	eng := NewClient(sock)
	if first, err := eng.List(context.Background()); err != nil {
		t.Fatal(err)
	} else {
		for _, c := range first {
			if c.CPUPct != nil {
				t.Fatalf("first List must not report CPU: %+v", c)
			}
		}
	}
	f.Mu.Lock() // counters advance: Δcpu 1e9 over Δsystem 10e9 on 4 CPUs = 40%
	f.Containers[id].CPUTotal, f.Containers[id].CPUSystem = 3_000_000_000, 110_000_000_000
	f.Mu.Unlock()
	list, err := eng.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 containers (exited included), got %d", len(list))
	}
	by := map[string]model.Container{}
	for _, c := range list {
		by[c.Name] = c
	}
	c := by["jobhunter-bot"]
	if c.ID != id || c.Image != "jobhunter-bot-jobhunter" || c.State != "running" || c.Status != "Up 3 hours" ||
		c.Project != "jobhunter-bot" || c.Health != "healthy" || !c.StartedAt.Equal(started) {
		t.Fatalf("bad container %+v", c)
	}
	if c.CPUPct == nil || math.Abs(*c.CPUPct-40) > 1e-9 {
		t.Fatalf("cpu %v", c.CPUPct)
	}
	if c.MemUsed == nil || *c.MemUsed != 60<<20 || c.MemLimit == nil || *c.MemLimit != 512<<20 {
		t.Fatalf("mem %v %v", c.MemUsed, c.MemLimit)
	}
	if len(c.Ports) != 1 || c.Ports[0] != "127.0.0.1:18080->8080/tcp" {
		t.Fatalf("ports %v", c.Ports)
	}
	e := by["old-job"]
	if e.State != "exited" || e.CPUPct != nil || e.MemUsed != nil || e.MemLimit != nil || !e.StartedAt.IsZero() || e.Health != "" {
		t.Fatalf("exited container should have no live data: %+v", e)
	}
}

func TestClientListStatsFailureLeavesNil(t *testing.T) {
	sock, f := NewFakeEngine(t)
	id := seededID(f)
	f.Mu.Lock()
	f.Containers[id].StatsStatus = 500
	f.Mu.Unlock()
	list, err := NewClient(sock).List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}
	if list[0].CPUPct != nil || list[0].MemUsed != nil || list[0].MemLimit != nil {
		t.Fatalf("stats should be nil: %+v", list[0])
	}
}

func TestClientListRequestsAllAndBoundsConcurrency(t *testing.T) {
	var cur, peak atomic.Int32
	var allQuery atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.45/containers/json", func(w http.ResponseWriter, r *http.Request) {
		allQuery.Store(r.URL.Query().Get("all"))
		out := "["
		for i := 0; i < 20; i++ {
			if i > 0 {
				out += ","
			}
			out += `{"Id":"c` + string(rune('a'+i)) + `","Names":["/n"],"State":"running"}`
		}
		w.Write([]byte(out + "]"))
	})
	mux.HandleFunc("/v1.45/containers/", func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		defer cur.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		w.Write([]byte(`{}`))
	})
	sock := serveUnix(t, mux)
	list, err := NewClient(sock).List(context.Background())
	if err != nil || len(list) != 20 {
		t.Fatalf("list %d %v", len(list), err)
	}
	if allQuery.Load() != "1" {
		t.Fatalf("all = %v", allQuery.Load())
	}
	if peak.Load() > 8 || peak.Load() < 2 {
		t.Fatalf("peak concurrency %d", peak.Load())
	}
}

func serveUnix(t *testing.T, h http.Handler) string {
	t.Helper()
	d, err := os.MkdirTemp("", "hw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	p := filepath.Join(d, "d.sock")
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return p
}

func TestClientDo(t *testing.T) {
	sock, f := NewFakeEngine(t)
	id := seededID(f)
	e := NewClient(sock)
	ctx := context.Background()
	state := func() string {
		f.Mu.Lock()
		defer f.Mu.Unlock()
		return f.Containers[id].State
	}
	if err := e.Do(ctx, id, model.ActionStop); err != nil || state() != "exited" {
		t.Fatalf("stop: %v %q", err, state())
	}
	if err := e.Do(ctx, id, model.ActionStop); err != nil { // 304
		t.Fatalf("stop already stopped: %v", err)
	}
	if err := e.Do(ctx, id, model.ActionStart); err != nil || state() != "running" {
		t.Fatalf("start: %v %q", err, state())
	}
	if err := e.Do(ctx, id, model.ActionStart); err != nil { // 304
		t.Fatalf("start already started: %v", err)
	}
	if err := e.Do(ctx, id, model.ActionRestart); err != nil {
		t.Fatalf("restart: %v", err)
	}
	err := e.Do(ctx, "nope", model.ActionRestart)
	var ae *model.APIError
	if !errors.As(err, &ae) || ae.Code != "not_found" {
		t.Fatalf("want not_found APIError, got %v", err)
	}
}

func TestClientDoHitsRestartPath(t *testing.T) {
	var got atomic.Value
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Method + " " + r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := NewClient(sock).Do(context.Background(), "abc123", model.ActionRestart); err != nil {
		t.Fatal(err)
	}
	if got.Load() != "POST /v1.45/containers/abc123/restart" {
		t.Fatalf("request %v", got.Load())
	}
}

func TestClientDoErrorCarriesDockerMessage(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"message":"driver failed"}`))
	}))
	err := NewClient(sock).Do(context.Background(), "abc", model.ActionStart)
	if err == nil || !strings.Contains(err.Error(), "driver failed") {
		t.Fatalf("err %v", err)
	}
}

func TestClientRejectsInvalidIDWithoutRequest(t *testing.T) {
	var hits atomic.Int32
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	e := NewClient(sock)
	for _, id := range []string{"../x", "", "-lead", "a/b", "a b", "a?x=1", "a%2fb"} {
		err := e.Do(context.Background(), id, model.ActionRestart)
		var ae *model.APIError
		if !errors.As(err, &ae) || ae.Code != "bad_request" {
			t.Fatalf("Do(%q): %v", id, err)
		}
		if _, err := e.Logs(context.Background(), id, 10, false); err == nil {
			t.Fatalf("Logs(%q) should fail", id)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests reached the daemon", hits.Load())
	}
}

func TestClientDaemonUnavailable(t *testing.T) {
	e := NewClient(filepath.Join(t.TempDir(), "missing.sock"))
	_, err := e.List(context.Background())
	var ae *model.APIError
	if !errors.As(err, &ae) || ae.Code != "docker_unavailable" {
		t.Fatalf("err %v", err)
	}
}

func TestClientLogsMultiplexed(t *testing.T) {
	sock, f := NewFakeEngine(t)
	id := seededID(f)
	f.Mu.Lock()
	f.LogFrames = [][]byte{
		LogFrame(1, "2026-10-15T08:30:00Z hello\n"),
		LogFrame(2, "2026-10-15T08:30:01Z \x1b[31mbad\x1b[0m\n"),
	}
	f.Mu.Unlock()
	ch, err := NewClient(sock).Logs(context.Background(), id, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	lines := collect(t, ch)
	want := []model.LogLine{{Stream: "stdout", Text: "hello"}, {Stream: "stderr", Text: "bad"}}
	if !sameText(lines, want) || lines[0].Time.IsZero() {
		t.Fatalf("got %+v", lines)
	}
}

func TestClientLogsTTY(t *testing.T) {
	sock, f := NewFakeEngine(t)
	id := seededID(f)
	f.Mu.Lock()
	f.Containers[id].Tty = true
	f.LogFrames = [][]byte{LogFrame(1, "2026-10-15T08:30:00Z tty line\r\n")}
	f.Mu.Unlock()
	ch, err := NewClient(sock).Logs(context.Background(), id, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	lines := collect(t, ch)
	if !sameText(lines, []model.LogLine{{Stream: "stdout", Text: "tty line"}}) {
		t.Fatalf("got %+v", lines)
	}
}

func TestClientLogsFollowAndTail(t *testing.T) {
	sock, f := NewFakeEngine(t)
	id := seededID(f)
	f.Mu.Lock()
	f.LogFrames = [][]byte{LogFrame(1, "one\n"), LogFrame(1, "two\n"), LogFrame(1, "three\n")}
	f.Mu.Unlock()
	e := NewClient(sock)

	ch, err := e.Logs(context.Background(), id, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := collect(t, ch); !sameText(got, []model.LogLine{{Stream: "stdout", Text: "two"}, {Stream: "stdout", Text: "three"}}) {
		t.Fatalf("tail got %+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err = e.Logs(ctx, id, -1, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("no line")
		}
	}
	select {
	case l, ok := <-ch:
		t.Fatalf("follow stream should stay open, got %+v %v", l, ok)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected line")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

func TestClientLogsUnknownContainer(t *testing.T) {
	sock, _ := NewFakeEngine(t)
	_, err := NewClient(sock).Logs(context.Background(), "nope", 10, false)
	var ae *model.APIError
	if !errors.As(err, &ae) || ae.Code != "not_found" {
		t.Fatalf("err %v", err)
	}
}

func TestClientCPUBaselineResetAndPrune(t *testing.T) {
	sock, f := NewFakeEngine(t)
	id := seededID(f)
	e := NewClient(sock)
	ctx := context.Background()
	set := func(total, system uint64) {
		f.Mu.Lock()
		f.Containers[id].CPUTotal, f.Containers[id].CPUSystem = total, system
		f.Mu.Unlock()
	}
	cpu := func() *float64 {
		l, err := e.List(ctx)
		if err != nil || len(l) != 1 {
			t.Fatalf("list %v %v", l, err)
		}
		return l[0].CPUPct
	}
	set(1_000_000_000, 10_000_000_000)
	if cpu() != nil {
		t.Fatal("first sight should be nil")
	}
	set(2_000_000_000, 20_000_000_000)
	if p := cpu(); p == nil || math.Abs(*p-40) > 1e-9 {
		t.Fatalf("second sample %v", p)
	}
	set(1_500_000_000, 30_000_000_000) // counter decrease (container restarted)
	if cpu() != nil {
		t.Fatal("counter decrease should be nil")
	}
	set(2_500_000_000, 40_000_000_000)
	if p := cpu(); p == nil || math.Abs(*p-40) > 1e-9 {
		t.Fatalf("after reset %v", p)
	}

	// Removed from the daemon: baseline pruned, so a re-appearing id starts over.
	f.Mu.Lock()
	ct := f.Containers[id]
	delete(f.Containers, id)
	f.Containers["other"] = &FakeContainer{ID: "other", Name: "o", State: "running"}
	f.Mu.Unlock()
	if _, err := e.List(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(e.(*client).prev); n != 1 {
		t.Fatalf("baselines kept: %d", n)
	}
	f.Mu.Lock()
	f.Containers[id] = ct
	delete(f.Containers, "other")
	f.Mu.Unlock()
	set(3_000_000_000, 50_000_000_000)
	if cpu() != nil {
		t.Fatal("re-appearing id should start over")
	}
}
