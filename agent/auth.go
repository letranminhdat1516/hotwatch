package agent

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxFailures    = 5
	failureWindow  = time.Minute
	lockoutLength  = time.Minute
	maxTrackedAddr = 4096
)

// SHA256Hex returns the lowercase hex SHA-256 of s; the agent stores only this
// digest of its token.
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// checkToken reports whether the request carries the configured bearer token.
// An unconfigured digest rejects everything.
func checkToken(r *http.Request, wantHex string) bool {
	if len(wantHex) != sha256.Size*2 {
		return false
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(SHA256Hex(got)), []byte(strings.ToLower(wantHex))) == 1
}

// remoteIP is the peer address without port (identity key for lockout).
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type lockEntry struct {
	fails       []time.Time
	lockedUntil time.Time
}

// lockout tracks failed authentications per source IP.
type lockout struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]*lockEntry
}

func newLockout(now func() time.Time) *lockout {
	return &lockout{now: now, m: map[string]*lockEntry{}}
}

// locked reports whether ip is currently locked out.
func (l *lockout) locked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[ip]
	return e != nil && l.now().Before(e.lockedUntil)
}

// fail records a failed auth; the maxFailures-th within failureWindow locks ip.
func (l *lockout) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.m) >= maxTrackedAddr {
		l.sweep(now)
	}
	e := l.m[ip]
	if e == nil {
		e = &lockEntry{}
		l.m[ip] = e
	}
	if now.Before(e.lockedUntil) {
		return
	}
	kept := e.fails[:0]
	for _, t := range e.fails {
		if now.Sub(t) < failureWindow {
			kept = append(kept, t)
		}
	}
	e.fails = append(kept, now)
	if len(e.fails) >= maxFailures {
		e.lockedUntil = now.Add(lockoutLength)
		e.fails = nil
	}
}

// sweep drops entries with nothing left to remember; if still full, drops
// the oldest-unlocked arbitrary entries so memory stays bounded.
func (l *lockout) sweep(now time.Time) {
	for ip, e := range l.m {
		if !now.Before(e.lockedUntil) && (len(e.fails) == 0 || now.Sub(e.fails[len(e.fails)-1]) >= failureWindow) {
			delete(l.m, ip)
		}
	}
	for ip, e := range l.m {
		if len(l.m) < maxTrackedAddr/2 {
			break
		}
		if !now.Before(e.lockedUntil) {
			delete(l.m, ip)
		}
	}
}
