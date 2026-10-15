package transport

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	sshCloseWait      = 3 * time.Second
	sshConnectTimeout = 10 * time.Second
	sshKeepaliveEvery = 15 * time.Second
	sshKeepaliveMiss  = 2
)

// SSHConfig describes how to reach one host's agent through an SSH tunnel.
type SSHConfig struct {
	Host      string
	Port      int // SSH port; 0 means 22
	User      string
	KeyPath   string // private key file; empty tries ~/.ssh/id_ed25519, id_ecdsa, id_rsa
	AgentPort int    // agent HTTP port on the remote loopback
	// KnownHostsFiles are read-only files (e.g. ~/.ssh/known_hosts); a missing
	// file is treated as empty. None are implied.
	KnownHostsFiles []string
	// AppKnownHosts is the app's own file: read like the others and the only
	// one TrustHostKey writes.
	AppKnownHosts string
	// Passphrase is asked for when a key file is encrypted. Nil skips such keys.
	// ctx is the dial's: it is done when the caller gives up, and the prompt
	// should then end. Passphrases are never stored or logged.
	Passphrase func(ctx context.Context, keyPath string) (string, error)
}

// sshSession is one live *ssh.Client plus what must be released with it.
type sshSession struct {
	client    *ssh.Client
	agentConn net.Conn
	stop      chan struct{}
	stopOnce  sync.Once
}

func (s *sshSession) close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.client.Close()
	if s.agentConn != nil {
		s.agentConn.Close()
	}
}

type sshDialer struct {
	cfg SSHConfig

	// life is done once Close is called; every connect and passphrase prompt
	// ends with it, since http.Transport detaches the dial's own ctx.
	life     context.Context
	stopLife context.CancelFunc

	keepaliveEvery time.Duration // tests shorten this

	handshakeTimeout time.Duration // tests shorten this
	closeWait        time.Duration // how long Close waits for an in-flight connect; tests shorten this
	dialSem          chan struct{} // serialises connects; waiters honour ctx
	decrypted        []ssh.Signer  // guarded by dialSem; memory only
	// promptMu guards prompted only and is never held across I/O or a prompt,
	// so ResetPrompts never waits for a connect.
	promptMu sync.Mutex
	prompted map[string]bool
	mu       sync.Mutex // guards the fields below
	sess     *sshSession
	closed   bool
}

// NewSSH returns a Dialer that tunnels to 127.0.0.1:AgentPort on the host over
// one reused SSH connection. It opens only direct-tcpip channels.
func NewSSH(cfg SSHConfig) Dialer {
	life, stop := context.WithCancel(context.Background())
	return &sshDialer{cfg: cfg, life: life, stopLife: stop, keepaliveEvery: sshKeepaliveEvery, handshakeTimeout: sshConnectTimeout, closeWait: sshCloseWait, dialSem: make(chan struct{}, 1), prompted: map[string]bool{}}
}

var errDialerClosed = errors.New("ssh dialer closed")

func (d *sshDialer) DialAgent(ctx context.Context) (net.Conn, error) {
	target := net.JoinHostPort("127.0.0.1", strconv.Itoa(d.cfg.AgentPort))
	s, err := d.session(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := s.client.DialContext(ctx, "tcp", target)
	if err == nil {
		return conn, nil
	}
	var oce *ssh.OpenChannelError
	if ctx.Err() != nil || errors.As(err, &oce) {
		return nil, err // client is alive: the agent port refused or caller gave up
	}
	// Dead client: drop it and redial once.
	d.drop(s)
	s, err = d.session(ctx)
	if err != nil {
		return nil, err
	}
	return s.client.DialContext(ctx, "tcp", target)
}

func (d *sshDialer) Close() error {
	d.stopLife()
	d.mu.Lock()
	s := d.sess
	d.sess = nil
	d.closed = true
	d.mu.Unlock()
	if s != nil {
		s.close()
	}
	// Drop cached decrypted keys. If a connect is in flight and holds the
	// semaphore for too long, it clears the cache itself on the way out.
	select {
	case d.dialSem <- struct{}{}:
		d.decrypted = nil
		<-d.dialSem
	case <-time.After(d.closeWait):
	}
	d.promptMu.Lock()
	d.prompted = nil
	d.promptMu.Unlock()
	return nil
}

var _ PromptResetter = (*sshDialer)(nil)

// ResetPrompts forgets which key files were already prompted for, so the next
// connect may ask once more for a declined key. It never blocks: a prompt open
// right now stays open, and if it is declined the next connect asks again.
// Decrypted keys are kept; a closed dialer stays cleared.
func (d *sshDialer) ResetPrompts() {
	d.promptMu.Lock()
	if d.prompted != nil {
		d.prompted = map[string]bool{}
	}
	d.promptMu.Unlock()
}

// markPrompted records that path is being asked for (even if declined).
func (d *sshDialer) markPrompted(path string) {
	d.promptMu.Lock()
	if d.prompted != nil {
		d.prompted[path] = true
	}
	d.promptMu.Unlock()
}

func (d *sshDialer) wasPrompted(path string) bool {
	d.promptMu.Lock()
	defer d.promptMu.Unlock()
	return d.prompted[path]
}

func (d *sshDialer) current() *sshSession {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sess
}

func (d *sshDialer) drop(s *sshSession) {
	d.mu.Lock()
	if d.sess == s {
		d.sess = nil
	}
	d.mu.Unlock()
	s.close()
}

func (d *sshDialer) session(ctx context.Context) (*sshSession, error) {
	d.mu.Lock()
	s, closed := d.sess, d.closed
	d.mu.Unlock()
	if closed {
		return nil, errDialerClosed
	}
	if s != nil {
		return s, nil
	}

	select {
	case d.dialSem <- struct{}{}:
		defer func() { <-d.dialSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.life.Done():
		return nil, errDialerClosed
	}
	d.mu.Lock()
	s, closed = d.sess, d.closed
	d.mu.Unlock()
	if closed {
		return nil, errDialerClosed
	}
	if s != nil {
		return s, nil
	}
	cctx, cancel := withLife(ctx, d.life)
	s, err := d.connect(cctx)
	cancel()
	if err != nil && d.life.Err() != nil && ctx.Err() == nil {
		err = errDialerClosed
	}
	d.mu.Lock()
	if d.closed {
		d.decrypted = nil // Close could not take the semaphore
	}
	d.mu.Unlock()
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		s.close()
		return nil, errDialerClosed
	}
	d.sess = s
	d.mu.Unlock()
	go d.keepalive(s)
	return s, nil
}

func (d *sshDialer) connect(ctx context.Context) (*sshSession, error) {
	cfg := d.cfg
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(port))

	files := append(append([]string(nil), cfg.KnownHostsFiles...), cfg.AppKnownHosts)
	checker, err := newHostKeyChecker(files)
	if err != nil {
		return nil, err
	}
	username := cfg.User
	if username == "" {
		u, err := user.Current()
		if err != nil {
			return nil, fmt.Errorf("ssh: no user configured: %w", err)
		}
		username = u.Username
	}

	// First attempt: agent keys, cached decrypted keys and key files that
	// need no passphrase. Passphrases are asked only if that fails.
	plain, encrypted, keyErr := d.loadFileKeys()
	s, verified, err := d.handshake(ctx, address, username, checker, append(append([]ssh.Signer(nil), d.decrypted...), plain...))
	if err == nil || !isAuthFailure(err, verified) || len(encrypted) == 0 || cfg.Passphrase == nil {
		return s, wrapKeyErr(err, keyErr, verified)
	}
	// Prompt outside any handshake deadline, then reconnect once.
	added := false
	for _, k := range encrypted {
		d.markPrompted(k.path) // never ask twice, even if declined
		pass, perr := cfg.Passphrase(ctx, k.path)
		if ctx.Err() != nil {
			break
		}
		if perr != nil {
			log.Printf("ssh: passphrase for %s not provided: %v", k.path, perr)
			continue
		}
		signer, perr := ssh.ParsePrivateKeyWithPassphrase(k.raw, []byte(pass))
		if perr != nil {
			log.Printf("ssh: cannot decrypt %s: %v", k.path, perr)
			continue
		}
		d.decrypted = append(d.decrypted, signer)
		added = true
	}
	if !added || ctx.Err() != nil {
		return nil, wrapKeyErr(err, keyErr, verified)
	}
	s, verified, err = d.handshake(ctx, address, username, checker, append(append([]ssh.Signer(nil), d.decrypted...), plain...))
	return s, wrapKeyErr(err, keyErr, verified)
}

// isAuthFailure reports a genuine authentication failure: the host key was
// verified and accepted on this very attempt, and the error is neither a host
// key error nor a peer-sent disconnect. A peer can put any text in a
// disconnect before key exchange completes, so text alone proves nothing.
func isAuthFailure(err error, hostKeyVerified bool) bool {
	if err == nil || !hostKeyVerified {
		return false
	}
	var hk *HostKeyError
	if errors.As(err, &hk) || strings.Contains(err.Error(), "ssh: disconnect, reason") {
		return false
	}
	return strings.Contains(err.Error(), "unable to authenticate")
}

// wrapKeyErr explains an authentication failure with the key-file problem, if any.
func wrapKeyErr(err, keyErr error, verified bool) error {
	if keyErr != nil && isAuthFailure(err, verified) {
		return fmt.Errorf("%w (%v)", err, keyErr)
	}
	return err
}

// handshake dials and authenticates with agent signers followed by extra.
func (d *sshDialer) handshake(ctx context.Context, address, username string, checker *hostKeyChecker, extra []ssh.Signer) (*sshSession, bool, error) {
	agentConn := d.dialAgentSocket()
	// One "publickey" method: x/crypto tries only the first method of a
	// given name, so agent keys and file keys share one chain.
	signers := func() ([]ssh.Signer, error) {
		var all []ssh.Signer
		if agentConn != nil {
			if as, err := agent.NewClient(agentConn).Signers(); err == nil {
				all = append(all, as...)
			}
		}
		return append(all, extra...), nil
	}
	var verified atomic.Bool // this attempt's host key was accepted
	ccfg := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{ssh.PublicKeysCallback(signers)},
		// The callback stays attached to the live client and may run again
		// on rekey from the key-exchange goroutine, hence the atomic.
		HostKeyCallback: func(h string, r net.Addr, k ssh.PublicKey) error {
			if err := checker.callback()(h, r, k); err != nil {
				return err
			}
			verified.Store(true)
			return nil
		},
		HostKeyAlgorithms: checker.algorithms(address),
		Timeout:           d.handshakeTimeout,
	}

	nd := net.Dialer{Timeout: d.handshakeTimeout}
	nc, err := nd.DialContext(ctx, "tcp", address)
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, false, err
	}
	// Bound the handshake by the timeout and by ctx.
	nc.SetDeadline(time.Now().Add(d.handshakeTimeout))
	stopWatch := context.AfterFunc(ctx, func() { nc.Close() })
	sc, chans, reqs, err := ssh.NewClientConn(nc, address, ccfg)
	stopWatch()
	hostKeyVerified := verified.Load()
	if err != nil {
		nc.Close()
		if agentConn != nil {
			agentConn.Close()
		}
		if ctx.Err() != nil {
			return nil, hostKeyVerified, ctx.Err()
		}
		return nil, hostKeyVerified, err
	}
	nc.SetDeadline(time.Time{})
	return &sshSession{
		client:    ssh.NewClient(sc, chans, reqs),
		agentConn: agentConn,
		stop:      make(chan struct{}),
	}, true, nil
}

// dialAgentSocket connects to ssh-agent when SSH_AUTH_SOCK is set and reachable.
func (d *sshDialer) dialAgentSocket() net.Conn {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return nil
	}
	return c
}

type encryptedKey struct {
	path string
	raw  []byte
}

// loadFileKeys reads the configured key (or the default ones that exist).
// Unencrypted keys come back as signers; encrypted ones not yet prompted for
// are returned for a later, on-demand passphrase prompt. An unreadable
// explicit key is reported as keyErr, not fatal.
func (d *sshDialer) loadFileKeys() (plain []ssh.Signer, encrypted []encryptedKey, keyErr error) {
	var paths []string
	explicit := d.cfg.KeyPath != ""
	if explicit {
		paths = []string{d.cfg.KeyPath}
	} else if home, err := os.UserHomeDir(); err == nil {
		for _, n := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			paths = append(paths, filepath.Join(home, ".ssh", n))
		}
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			if explicit {
				log.Printf("ssh: skipping key %s: %v", p, err)
				keyErr = fmt.Errorf("read ssh key %s: %w", p, err)
			}
			continue
		}
		signer, err := ssh.ParsePrivateKey(raw)
		var pm *ssh.PassphraseMissingError
		switch {
		case errors.As(err, &pm):
			if !d.wasPrompted(p) {
				encrypted = append(encrypted, encryptedKey{p, raw})
			}
		case err != nil:
			if explicit {
				log.Printf("ssh: skipping key %s: %v", p, err)
				keyErr = fmt.Errorf("parse ssh key %s: %w", p, err)
			}
		default:
			plain = append(plain, signer)
		}
	}
	return plain, encrypted, keyErr
}

// keepalive pings the server and closes the client after sshKeepaliveMiss
// consecutive unanswered pings. It ends when the session closes.
func (d *sshDialer) keepalive(s *sshSession) {
	dead := make(chan struct{})
	go func() { s.client.Wait(); close(dead) }()
	t := time.NewTicker(d.keepaliveEvery)
	defer t.Stop()
	missed := 0
	for {
		select {
		case <-s.stop:
			return
		case <-dead:
			return
		case <-t.C:
		}
		reply := make(chan error, 1)
		go func() { _, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil); reply <- err }()
		select {
		case err := <-reply:
			if err != nil {
				d.drop(s)
				return
			}
			missed = 0
		case <-time.After(d.keepaliveEvery):
			missed++
			if missed >= sshKeepaliveMiss {
				d.drop(s)
				return
			}
		case <-s.stop:
			return
		case <-dead:
			return
		}
	}
}

// withLife returns a ctx that ends when ctx or life does.
func withLife(ctx, life context.Context) (context.Context, context.CancelFunc) {
	merged, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(life, cancel)
	return merged, func() { stop(); cancel() }
}
