package agent

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"
)

// DefaultConfigPath is where `hotwatch-agent init` writes and `run` reads.
const DefaultConfigPath = "/etc/hotwatch/agent.json"

// DefaultIntervalMs is the sampling interval written by init.
const DefaultIntervalMs = 2000

// Bounds for intervalMs.
const (
	MinIntervalMs = 500
	MaxIntervalMs = 30000
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// FileConfig is the on-disk agent configuration. It never holds the token,
// only its SHA-256.
type FileConfig struct {
	Listen      string `json:"listen"`
	TokenSHA256 string `json:"tokenSHA256"`
	ReadOnly    bool   `json:"readOnly"`
	IntervalMs  int    `json:"intervalMs"`
}

// NewToken returns a fresh 32-byte random token, base64url without padding.
// It panics if the system random source fails, as continuing would risk a
// predictable credential.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("hotwatch: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ValidDigest reports whether s is a lowercase 64-char hex SHA-256.
func ValidDigest(s string) bool { return digestPattern.MatchString(s) }

// LoadConfig reads and validates the config file. It refuses unknown fields
// (so a plain token cannot be smuggled in), a missing or malformed token
// digest, and a file readable by group or others.
func LoadConfig(path string) (FileConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileConfig{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return FileConfig{}, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return FileConfig{}, fmt.Errorf("%s is accessible by group or others (mode %04o); it must be 0600", path, st.Mode().Perm())
	}
	dec := json.NewDecoder(io.LimitReader(f, 64<<10))
	dec.DisallowUnknownFields()
	var c FileConfig
	if err := dec.Decode(&c); err != nil {
		return FileConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return FileConfig{}, fmt.Errorf("%s: unexpected data after JSON object", path)
	}
	if !ValidDigest(c.TokenSHA256) {
		return FileConfig{}, fmt.Errorf("%s: tokenSHA256 must be 64 lowercase hex characters", path)
	}
	if c.IntervalMs == 0 {
		c.IntervalMs = DefaultIntervalMs
	}
	if c.IntervalMs < MinIntervalMs || c.IntervalMs > MaxIntervalMs {
		return FileConfig{}, fmt.Errorf("%s: intervalMs must be between %d and %d", path, MinIntervalMs, MaxIntervalMs)
	}
	return c, nil
}

// WriteConfig writes cfg to path with mode 0600 through a temporary file in
// the same directory. Without replace it fails if path already exists.
func WriteConfig(path string, cfg FileConfig, replace bool) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if replace {
		// Keep the owner of the file being replaced (the service user must
		// still be able to read it after a root-run rotation).
		if st, err := os.Stat(path); err == nil {
			if sys, ok := st.Sys().(*syscall.Stat_t); ok {
				if err := tmp.Chown(int(sys.Uid), int(sys.Gid)); err != nil {
					tmp.Close()
					return fmt.Errorf("keeping owner of %s: %w", path, err)
				}
			}
		}
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if replace {
		return os.Rename(tmp.Name(), path)
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists", path)
		}
		return err
	}
	return nil
}

const reloadEvery = 5 * time.Second

// TokenReloader serves the current token digest and re-reads it from the
// config file when the file changes (checked at most every 5 s, lazily on
// use), so a rotation revokes the old token without a restart. A file that
// is unreadable or invalid keeps the previous digest: it never fails open.
type TokenReloader struct {
	path string
	clk  Clock
	logf func(format string, args ...any)

	mu      sync.Mutex
	digest  string
	checked time.Time
	seen    os.FileInfo
}

// NewTokenReloader starts from the digest already loaded from path.
func NewTokenReloader(path, digest string, clk Clock, logf func(string, ...any)) *TokenReloader {
	if clk == nil {
		clk = realClock{}
	}
	r := &TokenReloader{path: path, clk: clk, logf: logf, digest: digest, checked: clk.Now()}
	r.seen, _ = os.Stat(path)
	return r
}

// Digest returns the current token digest.
func (r *TokenReloader) Digest() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clk.Now()
	if now.Sub(r.checked) < reloadEvery {
		return r.digest
	}
	r.checked = now
	st, err := os.Stat(r.path)
	if err != nil {
		r.logf("config reload: %v; keeping previous token", err)
		return r.digest
	}
	if r.seen != nil && os.SameFile(r.seen, st) && r.seen.ModTime().Equal(st.ModTime()) && r.seen.Size() == st.Size() {
		return r.digest
	}
	r.seen = st
	c, err := LoadConfig(r.path)
	if err != nil {
		r.logf("config reload: %v; keeping previous token", err)
		return r.digest
	}
	r.digest = c.TokenSHA256
	return r.digest
}
