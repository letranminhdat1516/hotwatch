package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
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
	// Passphrases are never stored or logged.
	Passphrase func(keyPath string) (string, error)
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

	keepaliveEvery time.Duration // tests shorten this

	handshakeTimeout time.Duration // tests shorten this
	dialSem          chan struct{} // serialises connects; waiters honour ctx
	mu               sync.Mutex    // guards the fields below
	sess             *sshSession
	closed           bool
}

// NewSSH returns a Dialer that tunnels to 127.0.0.1:AgentPort on the host over
// one reused SSH connection. It opens only direct-tcpip channels.
func NewSSH(cfg SSHConfig) Dialer {
	return &sshDialer{cfg: cfg, keepaliveEvery: sshKeepaliveEvery, handshakeTimeout: sshConnectTimeout, dialSem: make(chan struct{}, 1)}
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
	d.mu.Lock()
	s := d.sess
	d.sess = nil
	d.closed = true
	d.mu.Unlock()
	if s != nil {
		s.close()
	}
	return nil
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
	s, err := d.connect(ctx)
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
	// Load (and decrypt) key files before dialing so a passphrase prompt
	// is not bounded by the handshake deadline.
	fileSigners, err := d.fileSigners()
	if err != nil {
		return nil, err
	}
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
		return append(all, fileSigners...), nil
	}
	ccfg := &ssh.ClientConfig{
		User:              username,
		Auth:              []ssh.AuthMethod{ssh.PublicKeysCallback(signers)},
		HostKeyCallback:   checker.callback(),
		HostKeyAlgorithms: checker.algorithms(address),
		Timeout:           d.handshakeTimeout,
	}

	nd := net.Dialer{Timeout: d.handshakeTimeout}
	nc, err := nd.DialContext(ctx, "tcp", address)
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		return nil, err
	}
	// Bound the handshake by the timeout and by ctx.
	nc.SetDeadline(time.Now().Add(d.handshakeTimeout))
	stopWatch := context.AfterFunc(ctx, func() { nc.Close() })
	sc, chans, reqs, err := ssh.NewClientConn(nc, address, ccfg)
	stopWatch()
	if err != nil {
		nc.Close()
		if agentConn != nil {
			agentConn.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	nc.SetDeadline(time.Time{})
	return &sshSession{
		client:    ssh.NewClient(sc, chans, reqs),
		agentConn: agentConn,
		stop:      make(chan struct{}),
	}, nil
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

// fileSigners loads the configured key, or the default ones that exist.
func (d *sshDialer) fileSigners() ([]ssh.Signer, error) {
	var paths []string
	explicit := d.cfg.KeyPath != ""
	if explicit {
		paths = []string{d.cfg.KeyPath}
	} else if home, err := os.UserHomeDir(); err == nil {
		for _, n := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			paths = append(paths, filepath.Join(home, ".ssh", n))
		}
	}
	var signers []ssh.Signer
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			if explicit {
				return nil, fmt.Errorf("read ssh key %s: %w", p, err)
			}
			continue
		}
		signer, err := ssh.ParsePrivateKey(raw)
		var pm *ssh.PassphraseMissingError
		if errors.As(err, &pm) {
			if d.cfg.Passphrase == nil {
				continue
			}
			pass, perr := d.cfg.Passphrase(p)
			if perr != nil {
				return signers, perr
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, []byte(pass))
		}
		if err != nil {
			if explicit {
				return nil, fmt.Errorf("parse ssh key %s: %w", p, err)
			}
			continue
		}
		signers = append(signers, signer)
	}
	return signers, nil
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
