package hub

import (
	"testing"
	"time"
)

func TestConnTransitions(t *testing.T) {
	now := time.Unix(1000, 0)
	c := Conn{State: Connecting}
	steps := []struct {
		ev   Event
		want ConnState
	}{
		{EvOK, Online}, {EvFail, Stale}, {EvFail, Stale}, {EvFail, Offline},
		{EvFail, Offline}, {EvOK, Online}, {EvUnauthorized, Unauthorized},
		{EvOK, Unauthorized}, {EvReset, Connecting}, {EvHostKeyUnknown, HostKeyUnknown},
		{EvOK, HostKeyUnknown}, {EvFail, HostKeyUnknown},
		{EvReset, Connecting}, {EvHostKeyMismatch, HostKeyMismatch}, {EvFail, HostKeyMismatch},
		{EvOK, HostKeyMismatch}, {EvReset, Connecting}, {EvIncompatible, Incompatible},
		{EvOK, Incompatible}, {EvFail, Incompatible}, {EvReset, Connecting},
		{EvFail, Offline}, {EvOK, Online}, {EvIncompatible, Incompatible},
	}
	for i, s := range steps {
		c = c.Next(s.ev, now)
		if c.State != s.want {
			t.Fatalf("step %d: %v → %v, want %v", i, s.ev, c.State, s.want)
		}
	}
}

func TestConnCounters(t *testing.T) {
	t0 := time.Unix(1000, 0)
	c := Conn{State: Connecting}
	c = c.Next(EvOK, t0)
	if c.Failures != 0 || !c.LastOK.Equal(t0) {
		t.Fatalf("after OK: %+v", c)
	}
	t1 := t0.Add(time.Second)
	c = c.Next(EvFail, t1)
	c = c.Next(EvFail, t1)
	if c.Failures != 2 || !c.LastOK.Equal(t0) || c.State != Stale {
		t.Fatalf("after 2 fails: %+v", c)
	}
	c = c.Next(EvFail, t1)
	if c.State != Offline || c.Failures != 3 || !c.NextTry.Equal(t1.Add(2*time.Second)) {
		t.Fatalf("3rd fail: %+v", c)
	}
	c = c.Next(EvFail, t1)
	if c.Failures != 4 || !c.NextTry.Equal(t1.Add(4*time.Second)) {
		t.Fatalf("4th fail: %+v", c)
	}
	t2 := t1.Add(time.Minute)
	c = c.Next(EvOK, t2)
	if c.State != Online || c.Failures != 0 || !c.LastOK.Equal(t2) || !c.NextTry.IsZero() {
		t.Fatalf("recover: %+v", c)
	}
	c = c.Next(EvUnauthorized, t2).Next(EvReset, t2)
	if c.State != Connecting || c.Failures != 0 || !c.NextTry.IsZero() || !c.LastOK.Equal(t2) {
		t.Fatalf("reset: %+v", c)
	}
	// First contact failing goes straight to Offline with the shortest backoff.
	c = Conn{State: Connecting}.Next(EvFail, t0)
	if c.State != Offline || !c.NextTry.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("connecting fail: %+v", c)
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{2, 4, 8, 16, 30, 60, 60}
	for i, w := range want {
		if got := Backoff(i + 1); got != w*time.Second {
			t.Errorf("Backoff(%d) = %v, want %v", i+1, got, w*time.Second)
		}
	}
	if got := Backoff(0); got != 2*time.Second {
		t.Errorf("Backoff(0) = %v", got)
	}
	if got := Backoff(1000); got != 60*time.Second {
		t.Errorf("Backoff(1000) = %v", got)
	}
}

func TestEventString(t *testing.T) {
	if EvHostKeyMismatch.String() != "EvHostKeyMismatch" || Event(99).String() != "Event(99)" {
		t.Fatal(EvHostKeyMismatch.String(), Event(99).String())
	}
}
