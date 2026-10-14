//go:build dockerint

package docker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"hotwatch/model"
)

const intSocket = "/var/run/docker.sock"

// rawCall talks to the real daemon for test setup and cleanup only.
func rawCall(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, apiBase+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := NewClient(intSocket).(*client).http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func TestIntegrationDockerLifecycle(t *testing.T) {
	var rnd [4]byte
	rand.Read(rnd[:])
	name := "hw-test-" + hex.EncodeToString(rnd[:])

	code, body := rawCall(t, "POST", "/containers/create?name="+name, map[string]any{
		"Image": "busybox:1.36",
		"Cmd":   []string{"sh", "-c", "while true; do echo tick; sleep 1; done"},
	})
	if code == http.StatusNotFound { // image not present locally
		if c, b := rawCall(t, "POST", "/images/create?fromImage=busybox&tag=1.36", nil); c != 200 {
			t.Fatalf("pull: %d %s", c, b)
		}
		code, body = rawCall(t, "POST", "/containers/create?name="+name, map[string]any{
			"Image": "busybox:1.36",
			"Cmd":   []string{"sh", "-c", "while true; do echo tick; sleep 1; done"},
		})
	}
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	var created struct{ Id string }
	if err := json.Unmarshal(body, &created); err != nil || created.Id == "" {
		t.Fatalf("create body %s", body)
	}
	id := created.Id
	t.Cleanup(func() { rawCall(t, "DELETE", "/containers/"+id+"?force=1&v=1", nil) })

	e := NewClient(intSocket)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	find := func() (model.Container, bool) {
		list, err := e.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range list {
			if c.ID == id {
				return c, true
			}
		}
		return model.Container{}, false
	}

	// Created but not started: listed, not running.
	if c, ok := find(); !ok || c.Name != name || c.State == "running" {
		t.Fatalf("created container: %+v ok=%v", c, ok)
	}
	if err := e.Do(ctx, id, model.ActionStart); err != nil {
		t.Fatal(err)
	}
	c, ok := find()
	if !ok || c.State != "running" || c.StartedAt.IsZero() || c.MemUsed == nil || c.MemLimit == nil {
		t.Fatalf("running container: %+v ok=%v", c, ok)
	}
	if err := e.Do(ctx, id, model.ActionRestart); err != nil {
		t.Fatal(err)
	}

	lctx, lcancel := context.WithTimeout(ctx, 15*time.Second)
	defer lcancel()
	ch, err := e.Logs(lctx, id, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	var got *model.LogLine
	for l := range ch {
		if strings.TrimSpace(l.Text) == "tick" {
			got = &l
			lcancel()
			break
		}
	}
	if got == nil || got.Stream != "stdout" || got.Time.IsZero() {
		t.Fatalf("tick line: %+v", got)
	}

	if err := e.Do(ctx, id, model.ActionStop); err != nil {
		t.Fatal(err)
	}
	if c, ok := find(); !ok || c.State != "exited" {
		t.Fatalf("stopped container: %+v ok=%v", c, ok)
	}
	if err := e.Do(ctx, id, model.ActionStop); err != nil { // already stopped: 304
		t.Fatal(err)
	}
}
