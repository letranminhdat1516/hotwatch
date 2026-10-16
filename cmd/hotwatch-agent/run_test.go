package main

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"hotwatch/internal/testenv"
)

func TestHTTPServerLimits(t *testing.T) {
	s := newHTTPServer(http.NotFoundHandler())
	if s.ReadTimeout != 5*time.Second || s.ReadHeaderTimeout != 5*time.Second || s.WriteTimeout != 10*time.Second ||
		s.IdleTimeout != 60*time.Second || s.MaxHeaderBytes != 16<<10 {
		t.Fatalf("limits = %v %v %v %v %d", s.ReadTimeout, s.ReadHeaderTimeout, s.WriteTimeout, s.IdleTimeout, s.MaxHeaderBytes)
	}
}

func TestOpenAuditAppendsWith0640AndFailsLoudly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	for i := 0; i < 2; i++ {
		f, err := openAudit(p)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString("x\n")
		f.Close()
	}
	b, _ := os.ReadFile(p)
	st, _ := os.Stat(p)
	if string(b) != "x\nx\n" || (testenv.UnixModes() && st.Mode().Perm()&0o027 != 0) {
		t.Fatalf("content %q mode %v", b, st.Mode())
	}
	if _, err := openAudit(filepath.Join(t.TempDir(), "missing", "audit.log")); err == nil {
		t.Fatal("open failure must be reported")
	}
}

func TestDockerProbeCachesFor30s(t *testing.T) {
	now := time.Unix(1000, 0)
	calls, up := 0, true
	p := &dockerProbe{now: func() time.Time { return now }, dial: func() bool { calls++; return up }}
	if !p.Reachable() || !p.Reachable() || calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	up = false
	now = now.Add(29 * time.Second)
	if !p.Reachable() {
		t.Fatal("cache expired early")
	}
	now = now.Add(2 * time.Second)
	if p.Reachable() || calls != 2 {
		t.Fatalf("not re-checked after 30 s (calls=%d)", calls)
	}
}

func TestHostInfoCaps(t *testing.T) {
	p := &dockerProbe{now: time.Now, dial: func() bool { return true }}
	hi := hostInfo(p, func() bool { return true })()
	if hi.OS != runtime.GOOS || hi.CPUCount < 1 || hi.Hostname == "" || !hi.Caps.Docker || !hi.Caps.GPU || !hi.Caps.Processes {
		t.Fatalf("%+v", hi)
	}
}
