package transport

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/sys/unix"
)

// hostKeyChecker verifies server host keys against a set of known_hosts files.
type hostKeyChecker struct {
	check ssh.HostKeyCallback
}

// newHostKeyChecker builds one checker from every file. A missing or empty
// file contributes nothing; a malformed line is skipped.
func newHostKeyChecker(files []string) (*hostKeyChecker, error) {
	var clean []byte
	for _, f := range files {
		if f == "" {
			continue
		}
		raw, err := os.ReadFile(f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read known_hosts %s: %w", f, err)
		}
		for _, line := range bytes.Split(raw, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 || line[0] == '#' {
				continue
			}
			fixed, ok := sanitizeLine(line)
			if !ok {
				continue // malformed line: skipped, not fatal
			}
			line = fixed
			clean = append(clean, line...)
			clean = append(clean, '\n')
		}
	}
	// knownhosts.New only reads files, so hand it the sanitised public data
	// through a private temp file removed immediately after parsing.
	tmp, err := os.CreateTemp("", "hotwatch-known-hosts-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(clean); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	cb, err := knownhosts.New(tmp.Name())
	if err != nil {
		return nil, err
	}
	return &hostKeyChecker{check: cb}, nil
}

type hostAddr string

func (hostAddr) Network() string  { return "tcp" }
func (a hostAddr) String() string { return string(a) }

// algorithms lists the host key algorithms we have on file for address, so
// the server offers a key type we can verify. Nil when the host is unknown.
func (c *hostKeyChecker) algorithms(address string) []string {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil
	}
	probe, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil
	}
	var ke *knownhosts.KeyError
	if !errors.As(c.check(address, hostAddr(address), probe), &ke) {
		return nil
	}
	var algs []string
	seen := map[string]bool{}
	add := func(a string) {
		if !seen[a] {
			seen[a] = true
			algs = append(algs, a)
		}
	}
	for _, k := range ke.Want {
		switch t := k.Key.Type(); t {
		case ssh.KeyAlgoRSA:
			add(ssh.KeyAlgoRSASHA512)
			add(ssh.KeyAlgoRSASHA256)
			add(ssh.KeyAlgoRSA)
		default:
			add(t)
		}
	}
	return algs
}

// callback returns the ssh host key callback translating knownhosts results
// into *HostKeyError.
func (c *hostKeyChecker) callback() ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := c.check(hostname, remote, key)
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) {
			return &HostKeyError{
				Host:        knownhosts.Normalize(hostname),
				Addr:        hostname,
				Fingerprint: ssh.FingerprintSHA256(key),
				Mismatch:    len(ke.Want) > 0,
				Key:         key,
			}
		}
		return err
	}
}

// nextWord splits at the first space or tab, like x/crypto/ssh/knownhosts.
func nextWord(line string) (word, rest string) {
	i := strings.IndexAny(line, "\t ")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.Trim(line[i:], " \t")
}

// sanitizeLine returns a line knownhosts.New will accept, or false to drop it.
// Lines whose host field knownhosts.New would reject are dropped, except
// @revoked lines: revocation is keyed by the key, not the host, so dropping
// one would silently un-revoke the key. Those keep the line with host "*".
func sanitizeLine(line []byte) ([]byte, bool) {
	if _, _, _, _, _, err := ssh.ParseKnownHosts(line); err != nil {
		return nil, false
	}
	s := string(line)
	marker := ""
	w, rest := nextWord(s)
	if w == "@revoked" || w == "@cert-authority" {
		marker, s = w, rest
	}
	host, rest := nextWord(s)
	if rest == "" {
		return nil, false
	}
	if validHost(host) {
		return line, true
	}
	if marker != "@revoked" {
		return nil, false
	}
	return []byte("@revoked * " + rest), true
}

// validHost mirrors the host-pattern checks knownhosts.New applies.
func validHost(host string) bool {
	if host == "" || host[0] == '@' {
		return false
	}
	if host[0] == '|' {
		parts := strings.Split(host, "|")
		if len(parts) != 4 || parts[1] != "1" {
			return false
		}
		_, e1 := base64.StdEncoding.DecodeString(parts[2])
		_, e2 := base64.StdEncoding.DecodeString(parts[3])
		return e1 == nil && e2 == nil
	}
	for _, p := range strings.Split(host, ",") {
		if p == "" {
			continue
		}
		p = strings.TrimPrefix(p, "!")
		if p == "" {
			return false
		}
		if p[0] == '[' {
			if _, _, err := net.SplitHostPort(p); err != nil {
				return false
			}
		}
	}
	return true
}

var (
	// ErrFingerprintMismatch: the key is not the one the user approved.
	ErrFingerprintMismatch = errors.New("host key does not match the approved fingerprint")
	// ErrHostKeyConflict: a known_hosts file already holds a different key for the host.
	ErrHostKeyConflict = errors.New("a different host key is already on file; remove it by hand")
)

// TrustHostKey records key for host:port in the app's own known_hosts file
// (created 0600 in a 0700 directory). It never touches ~/.ssh.
//
// The key must have the fingerprint the user approved, and no known_hosts file
// (the app file plus any otherKnownHosts) may hold a different key for the
// host: a changed key is a hard stop. Re-trusting an already-trusted key is a
// no-op.
func TrustHostKey(appKnownHosts, host string, port int, key ssh.PublicKey, approvedFingerprint string, otherKnownHosts ...string) error {
	if appKnownHosts == "" {
		return errors.New("trust host key: no app known_hosts path")
	}
	if key == nil {
		return errors.New("trust host key: no key")
	}
	if host == "" || strings.IndexFunc(host, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("*?!|,[]", r)
	}) >= 0 {
		return fmt.Errorf("trust host key: invalid host %q", host)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("trust host key: invalid port %d", port)
	}
	if ssh.FingerprintSHA256(key) != approvedFingerprint {
		return ErrFingerprintMismatch
	}
	if st, err := os.Lstat(appKnownHosts); err == nil && st.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("trust host key: %s is a symlink", appKnownHosts)
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	checker, err := newHostKeyChecker(append(append([]string(nil), otherKnownHosts...), appKnownHosts))
	if err != nil {
		return err
	}
	var ke *knownhosts.KeyError
	switch err := checker.check(address, hostAddr(address), key); {
	case err == nil:
		return nil // already trusted
	case errors.As(err, &ke) && len(ke.Want) > 0:
		return ErrHostKeyConflict
	case errors.As(err, &ke):
		// unknown: fall through and append
	default:
		return err
	}

	dir := filepath.Dir(appKnownHosts)
	if st, err := os.Lstat(dir); err == nil && st.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("trust host key: %s is a symlink", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(appKnownHosts, os.O_RDWR|os.O_APPEND|os.O_CREATE|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(address)}, key) + "\n"
	if st, err := f.Stat(); err == nil && st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, st.Size()-1); err == nil && last[0] != '\n' {
			line = "\n" + line
		}
	}
	if _, err := f.WriteString(line); err != nil {
		return err
	}
	return f.Close()
}
