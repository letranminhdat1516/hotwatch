package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"hotwatch/model"
)

const (
	apiBase       = "http://docker/v1.45"
	enrichLimit   = 8
	enrichTimeout = 2 * time.Second
	// statsTimeout is longer than enrichTimeout: with stream=0 a real daemon
	// takes about 2 s to return two CPU samples for a long-running container
	// (measured against the agent-box daemon).
	statsTimeout   = 4 * time.Second
	composeProject = "com.docker.compose.project"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type client struct {
	http *http.Client

	mu   sync.Mutex
	prev map[string]cpuSample // last CPU counters per container id; guarded by mu
}

// cpuSample is one reading of a container's cumulative CPU counters.
type cpuSample struct{ total, system uint64 }

// NewClient returns an Engine that talks to the Docker daemon on the unix
// socket at the given path, using Engine API v1.45. CPU % is computed from the
// difference between consecutive List calls, so the first List after creation
// (or after a container first appears) reports nil CPU. List is safe for
// concurrent use, but overlapping calls share and overwrite one baseline, so
// callers should poll from a single goroutine.
func NewClient(socket string) Engine {
	return &client{prev: map[string]cpuSample{}, http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}}
}

func apiErr(code, format string, args ...any) *model.APIError {
	return &model.APIError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func checkID(id string) error {
	if !idPattern.MatchString(id) {
		return apiErr("bad_request", "invalid container id")
	}
	return nil
}

// roundTrip performs a request and maps transport failures to docker_unavailable.
func (c *client) roundTrip(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, nil)
	if err != nil {
		return nil, apiErr("internal", "%v", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, apiErr("docker_unavailable", "%v", err)
	}
	return resp, nil
}

// statusError converts an unexpected response into an error carrying Docker's message.
func statusError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var m struct {
		Message string `json:"message"`
	}
	msg := strings.TrimSpace(string(b))
	if json.Unmarshal(b, &m) == nil && m.Message != "" {
		msg = m.Message
	}
	if msg == "" {
		msg = resp.Status
	}
	if resp.StatusCode == http.StatusNotFound {
		return apiErr("not_found", "%s", msg)
	}
	return apiErr("internal", "docker: %s (HTTP %d)", msg, resp.StatusCode)
}

func (c *client) getJSON(ctx context.Context, path string, v any) error {
	resp, err := c.roundTrip(ctx, http.MethodGet, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

type summary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
	Ports  []struct {
		IP          string
		PrivatePort int
		PublicPort  int
		Type        string
	} `json:"Ports"`
}

func (c *client) List(ctx context.Context) ([]model.Container, error) {
	var sums []summary
	if err := c.getJSON(ctx, "/containers/json?all=1", &sums); err != nil {
		return nil, err
	}
	out := make([]model.Container, len(sums))
	for i, s := range sums {
		ct := model.Container{ID: s.ID, Image: s.Image, State: s.State, Status: s.Status, Project: s.Labels[composeProject]}
		if len(s.Names) > 0 {
			ct.Name = strings.TrimPrefix(s.Names[0], "/")
		}
		seen := map[string]bool{}
		for _, p := range s.Ports {
			if p.PublicPort == 0 {
				continue
			}
			spec := fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type)
			if p.IP == "" {
				spec = fmt.Sprintf("%d->%d/%s", p.PublicPort, p.PrivatePort, p.Type)
			}
			// The daemon lists IPv4 and IPv6 bindings separately; show one per mapping.
			key := fmt.Sprintf("%d/%d/%s", p.PublicPort, p.PrivatePort, p.Type)
			if !seen[key] {
				seen[key] = true
				ct.Ports = append(ct.Ports, spec)
			}
		}
		out[i] = ct
	}

	var smu sync.Mutex
	samples := map[string]cpuSample{}
	sem := make(chan struct{}, enrichLimit)
	var wg sync.WaitGroup
	run := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			f()
		}()
	}
	for i := range out {
		if out[i].State != "running" || checkID(out[i].ID) != nil {
			continue
		}
		ct := &out[i]
		// Each goroutine writes disjoint fields of *ct.
		run(func() { c.fillInspect(ctx, ct) })
		run(func() {
			if smp, ok := c.fillStats(ctx, ct); ok {
				smu.Lock()
				samples[ct.ID] = smp
				smu.Unlock()
			}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Keep baselines only for containers sampled now; this prunes removed ids.
	c.mu.Lock()
	c.prev = samples
	c.mu.Unlock()
	return out, nil
}

func (c *client) fillInspect(ctx context.Context, ct *model.Container) {
	ctx, cancel := context.WithTimeout(ctx, enrichTimeout)
	defer cancel()
	var in struct {
		State struct {
			StartedAt string
			Health    *struct{ Status string }
		}
	}
	if c.getJSON(ctx, "/containers/"+ct.ID+"/json", &in) != nil {
		return
	}
	if t, err := time.Parse(time.RFC3339Nano, in.State.StartedAt); err == nil && !t.IsZero() && t.Year() > 1 {
		ct.StartedAt = t
	}
	if in.State.Health != nil {
		ct.Health = in.State.Health.Status
	}
}

// fillStats reads one-shot stats for ct and returns its CPU counters for use as the next baseline.
func (c *client) fillStats(ctx context.Context, ct *model.Container) (cpuSample, bool) {
	ctx, cancel := context.WithTimeout(ctx, enrichTimeout)
	defer cancel()
	var st struct {
		CPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
			Online int    `json:"online_cpus"`
		} `json:"cpu_stats"`
		Mem struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if c.getJSON(ctx, "/containers/"+ct.ID+"/stats?stream=0&one-shot=true", &st) != nil {
		return cpuSample{}, false
	}
	cur := cpuSample{st.CPU.Usage.Total, st.CPU.System}
	// CPU % = Δcpu / Δsystem × online_cpus × 100 against our previous sample;
	// unknown on first sight or when a counter did not advance.
	c.mu.Lock()
	prev, seen := c.prev[ct.ID]
	c.mu.Unlock()
	if seen && cur.total >= prev.total && cur.system > prev.system {
		online := st.CPU.Online
		if online == 0 {
			online = 1
		}
		pct := float64(cur.total-prev.total) / float64(cur.system-prev.system) * float64(online) * 100
		ct.CPUPct = &pct
	}
	if st.Mem.Limit > 0 {
		used := st.Mem.Usage
		cache, ok := st.Mem.Stats["inactive_file"] // cgroup v2
		if !ok {
			cache, ok = st.Mem.Stats["cache"] // cgroup v1
		}
		if ok {
			used -= min(used, cache)
		}
		limit := st.Mem.Limit
		ct.MemUsed, ct.MemLimit = &used, &limit
	}
	return cur, true
}

func (c *client) Do(ctx context.Context, id string, act model.ContainerAction) error {
	if err := checkID(id); err != nil {
		return err
	}
	switch act {
	case model.ActionStart, model.ActionStop, model.ActionRestart:
	default:
		return apiErr("bad_request", "unknown action %q", act)
	}
	resp, err := c.roundTrip(ctx, http.MethodPost, "/containers/"+id+"/"+string(act))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotModified: // 304: already in the requested state
		return nil
	}
	return statusError(resp)
}

func (c *client) Logs(ctx context.Context, id string, tail int, follow bool) (<-chan model.LogLine, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	tailArg := "all"
	if tail >= 0 {
		tailArg = strconv.Itoa(tail)
	}
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {tailArg}}
	if follow {
		q.Set("follow", "1")
	} else {
		q.Set("follow", "0")
	}

	// Config.Tty decides whether the stream is multiplexed; when inspect
	// fails, fall back to the response content type.
	tty, ttyKnown := false, false
	var in struct{ Config struct{ Tty bool } }
	if err := c.getJSON(ctx, "/containers/"+id+"/json", &in); err == nil {
		tty, ttyKnown = in.Config.Tty, true
	} else {
		var ae *model.APIError
		if errors.As(err, &ae) && (ae.Code == "not_found" || ae.Code == "docker_unavailable") {
			return nil, err
		}
	}

	resp, err := c.roundTrip(ctx, http.MethodGet, "/containers/"+id+"/logs?"+q.Encode())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, statusError(resp)
	}
	if !ttyKnown {
		tty = resp.Header.Get("Content-Type") == "application/vnd.docker.raw-stream"
	}
	return demuxStream(ctx, resp.Body, tty), nil
}
