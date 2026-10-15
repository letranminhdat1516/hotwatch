package docker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"hotwatch/model"
)

func socketClient(path string) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}
}

func get(t *testing.T, c *http.Client, path string) (int, []byte) {
	t.Helper()
	resp, err := c.Get("http://docker" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func post(t *testing.T, c *http.Client, path string) (int, []byte) {
	t.Helper()
	resp, err := c.Post("http://docker"+path, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestFakeEngineListSeed(t *testing.T) {
	sock, _ := NewFakeEngine(t)
	c := socketClient(sock)
	code, body := get(t, c, "/v1.45/containers/json?all=1")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	var list []struct {
		Id     string
		Names  []string
		Image  string
		State  string
		Status string
		Labels map[string]string
		Ports  []struct {
			PrivatePort int
			PublicPort  int
			Type        string
			IP          string
		}
		Created int64
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 container, got %d", len(list))
	}
	got := list[0]
	if got.Names[0] != "/jobhunter-bot" || got.Image != "jobhunter-bot-jobhunter" ||
		got.State != "running" || got.Labels["com.docker.compose.project"] != "jobhunter-bot" ||
		got.Id == "" || got.Status == "" || got.Created == 0 {
		t.Fatalf("unexpected container: %+v", got)
	}
}

func TestFakeEngineStats(t *testing.T) {
	sock, f := NewFakeEngine(t)
	c := socketClient(sock)
	f.Mu.Lock()
	var id string
	for k := range f.Containers {
		id = k
	}
	f.Mu.Unlock()
	code, body := get(t, c, "/v1.45/containers/"+id+"/stats?stream=0")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	var s struct {
		CPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs     int    `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64 `json:"usage"`
			Limit uint64 `json:"limit"`
		} `json:"memory_stats"`
	}
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatal(err)
	}
	if s.CPUStats.CPUUsage.TotalUsage <= s.PreCPUStats.CPUUsage.TotalUsage ||
		s.CPUStats.SystemCPUUsage <= s.PreCPUStats.SystemCPUUsage ||
		s.CPUStats.OnlineCPUs == 0 || s.MemoryStats.Usage == 0 || s.MemoryStats.Limit < s.MemoryStats.Usage {
		t.Fatalf("bad stats: %+v", s)
	}
}

func TestFakeEngineActions(t *testing.T) {
	sock, f := NewFakeEngine(t)
	c := socketClient(sock)
	f.Mu.Lock()
	var id string
	for k := range f.Containers {
		id = k
	}
	f.Mu.Unlock()

	state := func() string {
		f.Mu.Lock()
		defer f.Mu.Unlock()
		return f.Containers[id].State
	}
	if code, _ := post(t, c, "/v1.45/containers/"+id+"/stop"); code != 204 {
		t.Fatalf("stop status %d", code)
	}
	if state() != "exited" {
		t.Fatalf("after stop: %q", state())
	}
	if code, _ := post(t, c, "/v1.45/containers/"+id+"/start"); code != 204 || state() != "running" {
		t.Fatalf("start: %d %q", code, state())
	}
	if code, _ := post(t, c, "/v1.45/containers/"+id+"/restart"); code != 204 || state() != "running" {
		t.Fatalf("restart: %d %q", code, state())
	}
	code, body := post(t, c, "/v1.45/containers/nope/restart")
	if code != 404 {
		t.Fatalf("unknown id status %d", code)
	}
	var m struct{ Message string }
	if err := json.Unmarshal(body, &m); err != nil || m.Message != "No such container: nope" {
		t.Fatalf("body %q err %v", body, err)
	}
	if code, _ := get(t, c, "/v1.45/containers/nope/stats?stream=0"); code != 404 {
		t.Fatalf("stats unknown id status %d", code)
	}
	if code, _ := get(t, c, "/v1.45/containers/nope/logs?stdout=1"); code != 404 {
		t.Fatalf("logs unknown id status %d", code)
	}
}

func TestFakeEngineLogsFrames(t *testing.T) {
	sock, f := NewFakeEngine(t)
	c := socketClient(sock)
	f.Mu.Lock()
	var id string
	for k := range f.Containers {
		id = k
	}
	f.LogFrames = [][]byte{LogFrame(1, "hello\n"), LogFrame(2, "oops\n")}
	f.Mu.Unlock()

	code, body := get(t, c, "/v1.45/containers/"+id+"/logs?stdout=1&stderr=1&tail=10")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	want := string(LogFrame(1, "hello\n")) + string(LogFrame(2, "oops\n"))
	if string(body) != want {
		t.Fatalf("body %q want %q", body, want)
	}
	if len(body) < 8 || body[0] != 1 || binary.BigEndian.Uint32(body[4:8]) != 6 {
		t.Fatalf("bad header: %v", body[:8])
	}
}

func TestFakeEngineAPI(t *testing.T) {
	ctx := context.Background()
	f := &FakeEngineAPI{
		Containers: []model.Container{{ID: "a", Name: "a"}},
		Lines:      []model.LogLine{{Stream: "stdout", Text: "x"}, {Stream: "stderr", Text: "y"}},
	}
	var e Engine = f
	list, err := e.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("list %v %v", list, err)
	}
	if err := e.Do(ctx, "a", model.ActionRestart); err != nil {
		t.Fatal(err)
	}
	if len(f.Done) != 1 || f.Done[0] != "a:restart" {
		t.Fatalf("done %v", f.Done)
	}
	ch, err := e.Logs(ctx, "a", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for l := range ch {
		texts = append(texts, l.Text)
	}
	if strings.Join(texts, "") != "xy" {
		t.Fatalf("lines %v", texts)
	}

	// follow keeps the channel open until ctx is done
	cctx, cancel := context.WithCancel(ctx)
	ch, err = e.Logs(cctx, "a", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	<-ch
	<-ch
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel should close after ctx done")
	}

	f.Mu.Lock()
	f.Err = io.ErrUnexpectedEOF
	f.Mu.Unlock()
	if _, err := e.List(ctx); err != io.ErrUnexpectedEOF {
		t.Fatalf("List err %v", err)
	}
	if err := e.Do(ctx, "a", model.ActionStop); err != io.ErrUnexpectedEOF {
		t.Fatalf("Do err %v", err)
	}
	if _, err := e.Logs(ctx, "a", 1, false); err != io.ErrUnexpectedEOF {
		t.Fatalf("Logs err %v", err)
	}
}

func TestFakeEngineInspectLogsFollowTail(t *testing.T) {
	sock, f := NewFakeEngine(t)
	c := socketClient(sock)
	started := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	f.Mu.Lock()
	var id string
	for k, ct := range f.Containers {
		id = k
		ct.StartedAt, ct.Health = started, "healthy"
	}
	f.LogFrames = [][]byte{LogFrame(1, "a\n"), LogFrame(1, "b\n"), LogFrame(1, "c\n")}
	f.Mu.Unlock()

	code, body := get(t, c, "/v1.45/containers/"+id+"/json")
	if code != 200 {
		t.Fatalf("inspect status %d", code)
	}
	var in struct {
		State struct {
			StartedAt string
			Health    struct{ Status string }
		}
		Config struct{ Tty bool }
	}
	if err := json.Unmarshal(body, &in); err != nil || in.State.StartedAt != "2026-10-01T08:00:00Z" ||
		in.State.Health.Status != "healthy" || in.Config.Tty {
		t.Fatalf("inspect %+v err %v", in, err)
	}

	_, body = get(t, c, "/v1.45/containers/"+id+"/logs?stdout=1&tail=2")
	if string(body) != string(LogFrame(1, "b\n"))+string(LogFrame(1, "c\n")) {
		t.Fatalf("tail body %q", body)
	}

	// follow=1 holds the connection open until the request context ends
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://docker/v1.45/containers/"+id+"/logs?stdout=1&follow=1", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Fatalf("follow stream ended on its own: %q", b)
	}
	if len(b) != 3*10 {
		t.Fatalf("follow wrote %d bytes before ctx ended", len(b))
	}
}
