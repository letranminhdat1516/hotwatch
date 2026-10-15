package docker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hotwatch/model"
)

// FakeEngineAPI is an in-memory Engine for tests that do not need HTTP.
type FakeEngineAPI struct {
	Mu         sync.Mutex
	Containers []model.Container
	Done       []string        // "id:restart" records of Do calls
	Lines      []model.LogLine // sent by Logs, then the channel closes (unless follow and ctx not done: keep open until ctx done)
	Err        error           // returned by every method while non-nil
}

func (f *FakeEngineAPI) List(ctx context.Context) ([]model.Container, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]model.Container(nil), f.Containers...), nil
}

func (f *FakeEngineAPI) Do(ctx context.Context, id string, act model.ContainerAction) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Done = append(f.Done, id+":"+string(act))
	return nil
}

func (f *FakeEngineAPI) Logs(ctx context.Context, id string, tail int, follow bool) (<-chan model.LogLine, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	lines := append([]model.LogLine(nil), f.Lines...)
	ch := make(chan model.LogLine)
	go func() {
		defer close(ch)
		for _, l := range lines {
			select {
			case ch <- l:
			case <-ctx.Done():
				return
			}
		}
		if follow {
			<-ctx.Done()
		}
	}()
	return ch, nil
}

// FakePort is one entry of ContainerSummary.Ports.
type FakePort struct {
	PrivatePort int
	PublicPort  int
	Type        string
	IP          string
}

// FakeContainer is the fake daemon's record of one container.
type FakeContainer struct {
	ID      string
	Name    string // without the leading slash
	Image   string
	State   string // running, exited, ...
	Status  string
	Labels  map[string]string
	Ports   []FakePort
	Created int64

	StartedAt   time.Time // reported by /containers/{id}/json State.StartedAt (zero: Docker's "0001-01-01..." value)
	Health      string    // State.Health.Status; empty means no healthcheck
	Tty         bool      // Config.Tty; logs are then served unmultiplexed as raw-stream
	CPUTotal    uint64    // cpu_stats.cpu_usage.total_usage; zero means a default
	CPUSystem   uint64    // cpu_stats.system_cpu_usage; zero means a default
	StatsStatus int       // non-zero: /stats answers this HTTP status instead of stats
}

// FakeState is the mutable state behind NewFakeEngine's socket server.
type FakeState struct {
	Mu         sync.Mutex
	Containers map[string]*FakeContainer
	LogFrames  [][]byte // raw multiplexed frames written by the logs endpoint; tail=N serves the last N frames; follow=1 holds the connection open afterwards until the request ends
}

const fakeAPIPrefix = "/v1.45/containers/"

// NewFakeEngine serves the Engine API v1.45 subset on a unix socket and
// returns the socket path. It is seeded with one running container,
// jobhunter-bot.
func NewFakeEngine(t testing.TB) (socketPath string, f *FakeState) {
	t.Helper()
	const id = "3f1c2a9b7d5e4c6a8b0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b"
	f = &FakeState{Containers: map[string]*FakeContainer{
		id: {
			ID:      id,
			Name:    "jobhunter-bot",
			Image:   "jobhunter-bot-jobhunter",
			State:   "running",
			Status:  "Up 3 hours",
			Labels:  map[string]string{"com.docker.compose.project": "jobhunter-bot"},
			Ports:   []FakePort{{PrivatePort: 8080, PublicPort: 18080, Type: "tcp", IP: "127.0.0.1"}},
			Created: 1760000000,
		},
	}}

	// macOS limits unix socket paths to 104 bytes; t.TempDir() can be too long.
	dir := t.TempDir()
	if len(dir) > 80 {
		d, err := os.MkdirTemp("", "hw")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(d) })
		dir = d
	}
	socketPath = filepath.Join(dir, "d.sock")
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return socketPath, f
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (f *FakeState) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1.45/containers/json" && r.Method == http.MethodGet {
		f.list(w, r)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, fakeAPIPrefix)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "page not found"})
		return
	}
	id, op, ok := strings.Cut(rest, "/")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "page not found"})
		return
	}

	f.Mu.Lock()
	c, exists := f.Containers[id]
	if !exists {
		f.Mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "No such container: " + id})
		return
	}
	switch {
	case r.Method == http.MethodPost && (op == "start" || op == "restart"):
		already := op == "start" && c.State == "running"
		c.State, c.Status = "running", "Up Less than a second"
		if !already {
			c.StartedAt = time.Now()
		}
		f.Mu.Unlock()
		if already {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && op == "stop":
		already := c.State != "running"
		c.State, c.Status = "exited", "Exited (0) Less than a second ago"
		f.Mu.Unlock()
		if already {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && op == "json":
		state := map[string]any{
			"Status":    c.State,
			"Running":   c.State == "running",
			"StartedAt": c.StartedAt.UTC().Format(time.RFC3339Nano),
		}
		if c.Health != "" {
			state["Health"] = map[string]any{"Status": c.Health}
		}
		body := map[string]any{"Id": c.ID, "Name": "/" + c.Name, "State": state, "Config": map[string]any{"Tty": c.Tty}}
		f.Mu.Unlock()
		writeJSON(w, http.StatusOK, body)
	case r.Method == http.MethodGet && op == "stats":
		status, total, system := c.StatsStatus, c.CPUTotal, c.CPUSystem
		f.Mu.Unlock()
		if status != 0 {
			writeJSON(w, status, map[string]string{"message": "stats unavailable"})
			return
		}
		oneShot := r.URL.Query().Get("one-shot")
		writeJSON(w, http.StatusOK, fakeStats(total, system, oneShot == "1" || oneShot == "true"))
	case r.Method == http.MethodGet && op == "logs":
		frames := append([][]byte(nil), f.LogFrames...)
		tty := c.Tty
		f.Mu.Unlock()
		if n, err := strconv.Atoi(r.URL.Query().Get("tail")); err == nil && n >= 0 && n < len(frames) {
			frames = frames[len(frames)-n:]
		}
		if tty {
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
		} else {
			w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
		}
		w.WriteHeader(http.StatusOK)
		for _, fr := range frames {
			if tty {
				fr = fr[8:]
			}
			w.Write(fr)
		}
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		if v := r.URL.Query().Get("follow"); v == "1" || v == "true" {
			<-r.Context().Done()
		}
	default:
		f.Mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "page not found"})
	}
}

func (f *FakeState) list(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all")
	f.Mu.Lock()
	defer f.Mu.Unlock()
	out := []map[string]any{}
	for _, c := range f.Containers {
		if c.State != "running" && all != "1" && all != "true" {
			continue
		}
		ports := []map[string]any{}
		for _, p := range c.Ports {
			ports = append(ports, map[string]any{
				"IP": p.IP, "PrivatePort": p.PrivatePort, "PublicPort": p.PublicPort, "Type": p.Type,
			})
		}
		out = append(out, map[string]any{
			"Id": c.ID, "Names": []string{"/" + c.Name}, "Image": c.Image,
			"State": c.State, "Status": c.Status, "Ports": ports,
			"Labels": c.Labels, "Created": c.Created,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// fakeStats builds a stats document. One-shot responses carry no precpu_stats,
// like the real daemon's.
func fakeStats(total, system uint64, oneShot bool) map[string]any {
	if total == 0 {
		total = 2_000_000_000
	}
	if system == 0 {
		system = 100_000_000_000
	}
	m := map[string]any{
		"cpu_stats": map[string]any{
			"cpu_usage":        map[string]any{"total_usage": total},
			"system_cpu_usage": system,
			"online_cpus":      4,
		},
		"memory_stats": map[string]any{
			"usage": 64 << 20, "limit": 512 << 20,
			"stats": map[string]any{"inactive_file": 4 << 20},
		},
	}
	if !oneShot {
		m["precpu_stats"] = map[string]any{
			"cpu_usage":        map[string]any{"total_usage": 1_000_000_000},
			"system_cpu_usage": 90_000_000_000,
			"online_cpus":      4,
		}
	}
	return m
}

// LogFrame builds one Docker multiplexed log frame: an 8-byte header
// (stream type, 3 zero bytes, big-endian payload size) followed by the payload.
// Stream type 1 is stdout, 2 is stderr.
func LogFrame(stream byte, payload string) []byte {
	b := make([]byte, 8, 8+len(payload))
	b[0] = stream
	binary.BigEndian.PutUint32(b[4:], uint32(len(payload)))
	return append(b, payload...)
}

var _ Engine = (*FakeEngineAPI)(nil)
