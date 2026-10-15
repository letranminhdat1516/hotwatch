package agent

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// auditEntry is one JSON line. It never carries credentials.
type auditEntry struct {
	Time   time.Time `json:"time"`
	Remote string    `json:"remote"`
	Method string    `json:"method"`
	Route  string    `json:"route"`
	Target string    `json:"target"`
	Signal string    `json:"signal,omitempty"`
	Result int       `json:"result"`
}

type auditor struct {
	mu sync.Mutex
	w  io.Writer
}

func (a *auditor) write(e auditEntry) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.w.Write(append(b, '\n')); err != nil {
		log.Printf("hotwatch-agent: audit write failed: %v", err) // never drop silently; do not block the action
	}
}

// statusWriter records the response status for the audit line and passes
// Flush/Unwrap through so streaming works.
type statusWriter struct {
	http.ResponseWriter
	status int
	signal string // set by the signal handler for the audit line
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(p)
}

func (s *statusWriter) Flush() {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// clip bounds attacker-controlled text written to the audit log.
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
