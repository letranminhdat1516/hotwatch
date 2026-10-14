package hub

import (
	"fmt"
	"time"
)

// ConnState is a host connection's state (ARCHITECTURE.md "Host connection").
type ConnState string

const (
	Connecting      ConnState = "Connecting"
	Online          ConnState = "Online"
	Stale           ConnState = "Stale"
	Offline         ConnState = "Offline"
	Unauthorized    ConnState = "Unauthorized"
	Incompatible    ConnState = "Incompatible"
	HostKeyUnknown  ConnState = "HostKeyUnknown"
	HostKeyMismatch ConnState = "HostKeyMismatch"
)

// Event is an input to the connection state machine.
type Event int

const (
	EvOK Event = iota
	EvFail
	EvUnauthorized
	EvIncompatible
	EvHostKeyUnknown
	EvHostKeyMismatch
	EvReset
)

var eventNames = [...]string{"EvOK", "EvFail", "EvUnauthorized", "EvIncompatible", "EvHostKeyUnknown", "EvHostKeyMismatch", "EvReset"}

func (e Event) String() string {
	if e >= 0 && int(e) < len(eventNames) {
		return eventNames[e]
	}
	return fmt.Sprintf("Event(%d)", int(e))
}

// Conn is a host's connection state with its failure bookkeeping.
type Conn struct {
	State    ConnState
	Failures int // consecutive failures; reset by EvOK
	LastOK   time.Time
	NextTry  time.Time // meaningful only while Offline
}

// staleFailures is how many consecutive failures keep a host Stale.
const staleFailures = 2

// Next returns the connection after ev at time now.
//
// EvReset leaves any state for Connecting (clearing failures and the retry
// time, keeping LastOK). Unauthorized, Incompatible, HostKeyUnknown and
// HostKeyMismatch leave only on EvReset.
func (c Conn) Next(ev Event, now time.Time) Conn {
	if ev == EvReset {
		return Conn{State: Connecting, LastOK: c.LastOK}
	}
	switch c.State {
	case Unauthorized, Incompatible, HostKeyUnknown, HostKeyMismatch:
		return c
	}
	switch ev {
	case EvOK:
		return Conn{State: Online, LastOK: now}
	case EvFail:
		c.Failures++
		if c.State == Connecting || c.Failures > staleFailures {
			c.State = Offline
			c.NextTry = now.Add(Backoff(c.Failures - staleFailures))
		} else {
			c.State = Stale
		}
	case EvUnauthorized:
		c.State, c.NextTry = Unauthorized, time.Time{}
	case EvIncompatible:
		c.State, c.NextTry = Incompatible, time.Time{}
	case EvHostKeyUnknown:
		c.State, c.NextTry = HostKeyUnknown, time.Time{}
	case EvHostKeyMismatch:
		c.State, c.NextTry = HostKeyMismatch, time.Time{}
	}
	return c
}

var backoffSteps = [...]time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 60 * time.Second}

// Backoff is the retry delay after the nth Offline failure: 2s, 4s, 8s, 16s,
// 30s, then 60s. Values below 1 give the first step.
func Backoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	if failures > len(backoffSteps) {
		failures = len(backoffSteps)
	}
	return backoffSteps[failures-1]
}
