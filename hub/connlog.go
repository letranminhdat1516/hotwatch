package hub

import "time"

// LogEntry is one line of a host's connection log: the state it entered, or
// a new reason for staying in a failed one.
type LogEntry struct {
	Time   time.Time
	State  ConnState
	Detail string // the error, or what started the attempt; "" when Online
}

// maxLog is how many entries a host keeps, newest last.
const maxLog = 100

// note records the host's current state with detail when the state changed
// or a failure's reason did. The log is copied on write: a published slice is
// never changed afterwards.
func (p *host) note(detail string, now time.Time) {
	if n := len(p.log); n > 0 {
		last := p.log[n-1]
		if last.State == p.conn.State && (detail == "" || last.Detail == detail) {
			return
		}
	}
	keep := p.log[max(len(p.log)-(maxLog-1), 0):]
	next := make([]LogEntry, len(keep), len(keep)+1)
	copy(next, keep)
	p.log = append(next, LogEntry{Time: now, State: p.conn.State, Detail: detail})
}
