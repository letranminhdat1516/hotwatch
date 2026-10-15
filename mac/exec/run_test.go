package exec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSystemTimesOut(t *testing.T) {
	start := time.Now()
	_, err := System(50*time.Millisecond)(context.Background(), "sleep", "1")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "killed") {
		t.Fatalf("err = %v, want a deadline", err)
	}
	if d := time.Since(start); d > 400*time.Millisecond {
		t.Fatalf("took %v, the timeout is 50 ms", d)
	}
}

func TestSystemPassesArgvWithoutAShell(t *testing.T) {
	out, err := System(time.Second)(context.Background(), "echo", "$HOME", "a b")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "$HOME a b" {
		t.Fatalf("echo printed %q: a shell expanded the arguments", got)
	}
}

func TestSystemCapsOutput(t *testing.T) {
	out, err := System(5*time.Second)(context.Background(), "head", "-c", "10000000", "/dev/zero")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want too large", err)
	}
	if len(out) > MaxOutput {
		t.Fatalf("kept %d bytes, cap is %d", len(out), MaxOutput)
	}
}

func TestStreamingDeliversLinesThenExit(t *testing.T) {
	lines, wait, err := Streaming(context.Background(), "sh", "-c", "echo one; echo two 1>&2; exit 3")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for l := range lines {
		got = append(got, l)
	}
	if len(got) != 2 || got[0] != "one" && got[1] != "one" {
		t.Fatalf("lines %q", got)
	}
	werr := wait()
	if werr == nil || !strings.Contains(werr.Error(), "exit status 3") {
		t.Fatalf("wait = %v, want exit status 3", werr)
	}
}
