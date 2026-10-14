package transport

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
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
			if _, _, _, _, _, err := ssh.ParseKnownHosts(line); err != nil {
				continue // malformed line: skipped, not fatal
			}
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
				Fingerprint: ssh.FingerprintSHA256(key),
				Mismatch:    len(ke.Want) > 0,
			}
		}
		return err
	}
}

// TrustHostKey records key for host:port in the app's own known_hosts file
// (created 0600 in a 0700 directory). It never touches ~/.ssh.
func TrustHostKey(appKnownHosts, host string, port int, key ssh.PublicKey) error {
	if appKnownHosts == "" {
		return errors.New("trust host key: no app known_hosts path")
	}
	if host == "" || strings.IndexFunc(host, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == ',' }) >= 0 {
		return fmt.Errorf("trust host key: invalid host %q", host)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("trust host key: invalid port %d", port)
	}
	if err := os.MkdirAll(filepath.Dir(appKnownHosts), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(appKnownHosts, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{knownhosts.Normalize(net.JoinHostPort(host, strconv.Itoa(port)))}, key) + "\n"
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
