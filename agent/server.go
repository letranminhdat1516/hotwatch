// Package agent implements the hotwatch-agent HTTP contract (/v1).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"hotwatch/collect"
	"hotwatch/docker"
	"hotwatch/model"
)

const (
	maxBody     = 4 << 10
	defaultTail = 100
	maxTail     = 5000
)

// codeTooManyRequests is the APIError code for a locked-out source IP.
const codeTooManyRequests = "too_many_requests"

var containerID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// Signaller delivers a signal to a process, refusing when the process start
// time differs from start (PID reuse).
type Signaller interface {
	Signal(pid int32, start time.Time, s model.Signal) error
}

// Config configures the agent handler.
type Config struct {
	TokenSHA256 string        // hex SHA-256 of the bearer token; empty rejects every request
	ReadOnly    bool          // reject every POST with 403 forbidden_read_only
	Interval    time.Duration // sampling interval (used by the sampler added in a later task)

	// Info supplies the static host description for /v1/info. The server sets
	// ProtocolVersion and Caps.ReadOnly itself. Nil yields a minimal HostInfo.
	Info func() model.HostInfo
	// Now is the clock for lockout; nil means time.Now (injectable for tests).
	Now func() time.Time
	// ActionTimeout bounds container actions and signals, which are detached
	// from the client connection; zero means 60 s.
	ActionTimeout time.Duration
}

type server struct {
	cfg   Config
	col   collect.Collector
	eng   docker.Engine
	sig   Signaller
	audit *auditor
	lock  *lockout
	mux   *http.ServeMux
}

// New builds the agent handler. Chain: limitBody, lockout, auth, readOnly,
// route handler; every POST and every failed auth is audited.
func New(cfg Config, col collect.Collector, eng docker.Engine, sig Signaller, audit io.Writer) http.Handler {
	if cfg.ActionTimeout <= 0 {
		cfg.ActionTimeout = 60 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if audit == nil {
		audit = io.Discard
	}
	s := &server{cfg: cfg, col: col, eng: eng, sig: sig, audit: &auditor{w: audit}, lock: newLockout(cfg.Now), mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/info", s.info)
	s.mux.HandleFunc("GET /v1/snapshot", s.snapshot)
	s.mux.HandleFunc("GET /v1/processes", s.processes)
	s.mux.HandleFunc("POST /v1/processes/{pid}/signal", s.signal)
	s.mux.HandleFunc("GET /v1/containers", s.containers)
	s.mux.HandleFunc("POST /v1/containers/{id}/{action}", s.containerAction)
	s.mux.HandleFunc("GET /v1/containers/{id}/logs", s.logs)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, &model.APIError{Code: "not_found", Message: "no such route"})
	})
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sw := &statusWriter{ResponseWriter: w}
	w = sw
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	ip := remoteIP(r)
	authFailed := false
	defer func() {
		if r.Method == http.MethodPost || authFailed || sw.status == http.StatusTooManyRequests {
			_, route := s.mux.Handler(r)
			s.audit.write(auditEntry{Time: s.cfg.Now().UTC(), Remote: ip, Method: clip(r.Method, 16), Route: route, Target: clip(r.URL.Path, 256), Signal: sw.signal, Result: sw.status})
		}
	}()

	if !checkToken(r, s.cfg.TokenSHA256) {
		authFailed = true
		// A valid token is never throttled (SSH-tunnel clients all share
		// 127.0.0.1); only invalid attempts are counted and refused.
		if s.lock.locked(ip) {
			w.Header().Set("Retry-After", strconv.Itoa(int(lockoutLength/time.Second)))
			writeErr(w, &model.APIError{Code: codeTooManyRequests, Message: "too many failed authentications; try again later"})
			return
		}
		s.lock.fail(ip)
		writeErr(w, &model.APIError{Code: "unauthorized", Message: "missing or invalid token"})
		return
	}
	if s.cfg.ReadOnly && r.Method == http.MethodPost {
		writeErr(w, &model.APIError{Code: "forbidden_read_only", Message: "agent is read-only"})
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *server) info(w http.ResponseWriter, r *http.Request) {
	var hi model.HostInfo
	if s.cfg.Info != nil {
		hi = s.cfg.Info()
	}
	hi.ProtocolVersion = model.ProtocolVersion
	hi.Caps.ReadOnly = s.cfg.ReadOnly
	writeJSON(w, hi)
}

func (s *server) snapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := s.col.Sample(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, snap)
}

func (s *server) processes(w http.ResponseWriter, r *http.Request) {
	ps, err := s.col.Processes(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if ps == nil {
		ps = []model.Process{}
	}
	writeJSON(w, ps)
}

func (s *server) signal(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(r.PathValue("pid"), 10, 32)
	if err != nil || pid <= 1 {
		writeErr(w, badRequest("pid must be an integer greater than 1"))
		return
	}
	var body struct {
		Signal    string `json:"signal"`
		StartTime string `json:"startTime"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeErr(w, badRequest("invalid JSON body"))
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		writeErr(w, badRequest("unexpected data after JSON body"))
		return
	}
	var sig model.Signal
	switch body.Signal {
	case string(model.SigTerm):
		sig = model.SigTerm
	case string(model.SigKill):
		sig = model.SigKill
	default:
		writeErr(w, badRequest("signal must be TERM or KILL"))
		return
	}
	start, err := time.Parse(time.RFC3339Nano, body.StartTime)
	if err != nil {
		writeErr(w, badRequest("startTime must be RFC3339Nano"))
		return
	}
	if sw, ok := w.(*statusWriter); ok {
		sw.signal = string(sig)
	}
	s.extendDeadline(w)
	if err := s.sig.Signal(int32(pid), start, sig); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) containers(w http.ResponseWriter, r *http.Request) {
	cs, err := s.eng.List(r.Context())
	if err != nil {
		writeErr(w, dockerErr(err))
		return
	}
	if cs == nil {
		cs = []model.Container{}
	}
	writeJSON(w, cs)
}

// checkContainer validates id syntax and that it exists in the current list.
func (s *server) checkContainer(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !containerID.MatchString(id) {
		return "", badRequest("invalid container id")
	}
	cs, err := s.eng.List(r.Context())
	if err != nil {
		return "", dockerErr(err)
	}
	for _, c := range cs {
		if c.ID == id || c.Name == id {
			return id, nil
		}
	}
	return "", &model.APIError{Code: "not_found", Message: "no such container"}
}

func (s *server) containerAction(w http.ResponseWriter, r *http.Request) {
	var act model.ContainerAction
	switch r.PathValue("action") {
	case "start":
		act = model.ActionStart
	case "stop":
		act = model.ActionStop
	case "restart":
		act = model.ActionRestart
	default:
		writeErr(w, &model.APIError{Code: "not_found", Message: "no such route"})
		return
	}
	id, err := s.checkContainer(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := s.detached(w, r)
	defer cancel()
	if err := s.eng.Do(ctx, id, act); err != nil {
		writeErr(w, dockerErr(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) logs(w http.ResponseWriter, r *http.Request) {
	id, err := s.checkContainer(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	tail := defaultTail
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tail = min(max(n, 1), maxTail)
		} else if errors.Is(err, strconv.ErrRange) {
			tail = maxTail
		}
	}
	q := r.URL.Query().Get("follow")
	follow := q == "1" || q == "true"

	ctx := r.Context()
	ch, err := s.eng.Logs(ctx, id, tail, follow)
	if err != nil {
		writeErr(w, dockerErr(err))
		return
	}
	// Streams outlive the server's write timeout; cancellation comes from ctx.
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	rc.Flush()
	enc := json.NewEncoder(w)
	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-ch:
			if !ok {
				return
			}
			if enc.Encode(l) != nil {
				return
			}
			rc.Flush()
		}
	}
}

// detached returns a context that survives client disconnects (a half-done
// docker restart is worse than a late answer) but is bounded by ActionTimeout,
// and extends the write deadline to cover it.
func (s *server) detached(w http.ResponseWriter, r *http.Request) (context.Context, context.CancelFunc) {
	s.extendDeadline(w)
	return context.WithTimeout(context.WithoutCancel(r.Context()), s.cfg.ActionTimeout)
}

func (s *server) extendDeadline(w http.ResponseWriter) {
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(s.cfg.ActionTimeout + 5*time.Second))
}

func badRequest(msg string) *model.APIError {
	return &model.APIError{Code: "bad_request", Message: msg}
}

// dockerErr keeps *APIError as is and reports anything else as a Docker failure.
func dockerErr(err error) error {
	var ae *model.APIError
	if errors.As(err, &ae) {
		return err
	}
	return &model.APIError{Code: "docker_unavailable", Message: err.Error()}
}

var statusByCode = map[string]int{
	"bad_request":         http.StatusBadRequest,
	"unauthorized":        http.StatusUnauthorized,
	"forbidden":           http.StatusForbidden,
	"forbidden_read_only": http.StatusForbidden,
	"not_found":           http.StatusNotFound,
	"conflict":            http.StatusConflict,
	codeTooManyRequests:   http.StatusTooManyRequests,
	"docker_unavailable":  http.StatusBadGateway,
}

// writeErr sends err as an APIError; non-APIErrors become 500 "internal"
// without echoing their text.
func writeErr(w http.ResponseWriter, err error) {
	var ae *model.APIError
	if !errors.As(err, &ae) {
		var v model.APIError
		if errors.As(err, &v) {
			ae = &v
		} else {
			ae = &model.APIError{Code: "internal", Message: "internal error"}
		}
	}
	status, ok := statusByCode[ae.Code]
	if !ok {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ae)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
