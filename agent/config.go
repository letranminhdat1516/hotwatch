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
)

// DefaultConfigPath is where `hotwatch-agent init` writes and `run` reads.
const DefaultConfigPath = "/etc/hotwatch/agent.json"

// DefaultIntervalMs is the sampling interval written by init.
const DefaultIntervalMs = 2000

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
	if c.IntervalMs < 0 {
		return FileConfig{}, fmt.Errorf("%s: intervalMs must not be negative", path)
	}
	if c.IntervalMs == 0 {
		c.IntervalMs = DefaultIntervalMs
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
