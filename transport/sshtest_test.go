package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// testSSHServer is an in-process SSH server that accepts one client key,
// serves only direct-tcpip channels and forwards them to a local echo
// listener. It records what the client asked for.
type testSSHServer struct {
	t       *testing.T
	hostKey ssh.PublicKey
	signer  ssh.Signer
	cfg     *ssh.ServerConfig
	addr    string // host:port of the SSH listener
	host    string
	port    int

	echo     net.Listener
	keyPath  string // client private key file (accepted)
	mu       sync.Mutex
	ln       net.Listener
	conns    []net.Conn
	targets  []string // "host:port" of each direct-tcpip request
	sessions int      // "session" channel attempts
	other    []string // other channel types attempted
	swallow  bool     // do not answer keepalives
}

func newTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func startTestSSHServer(t *testing.T) *testSSHServer {
	t.Helper()
	s := &testSSHServer{t: t}
	s.signer = newTestSigner(t)
	s.hostKey = s.signer.PublicKey()

	// client key on disk (unencrypted OpenSSH format)
	_, cpriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(cpriv, "")
	if err != nil {
		t.Fatal(err)
	}
	s.keyPath = filepath.Join(t.TempDir(), "id_test")
	if err := os.WriteFile(s.keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	csigner, err := ssh.NewSignerFromKey(cpriv)
	if err != nil {
		t.Fatal(err)
	}
	accepted := csigner.PublicKey().Marshal()

	s.cfg = &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if string(k.Marshal()) == string(accepted) {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("unknown key")
		},
	}
	s.cfg.AddHostKey(s.signer)

	s.echo, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := s.echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	t.Cleanup(func() { s.echo.Close(); s.stop() })

	s.listen("127.0.0.1:0")
	return s
}

func (s *testSSHServer) echoPort() int { return s.echo.Addr().(*net.TCPAddr).Port }

func (s *testSSHServer) listen(addr string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.t.Fatal(err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	s.addr = ln.Addr().String()
	host, p, _ := net.SplitHostPort(s.addr)
	s.host = host
	s.port, _ = strconv.Atoi(p)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, c)
			s.mu.Unlock()
			go s.serve(c)
		}
	}()
}

// stop closes the listener and every live connection.
func (s *testSSHServer) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		s.ln.Close()
		s.ln = nil
	}
	for _, c := range s.conns {
		c.Close()
	}
	s.conns = nil
}

// restart stops the server and listens again on the same address.
func (s *testSSHServer) restart() {
	addr := s.addr
	s.stop()
	s.listen(addr)
}

func (s *testSSHServer) serve(nc net.Conn) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, s.cfg)
	if err != nil {
		nc.Close()
		return
	}
	defer conn.Close()
	go func() {
		for r := range reqs {
			s.mu.Lock()
			sw := s.swallow
			s.mu.Unlock()
			if r.WantReply && !sw {
				r.Reply(r.Type == "keepalive@openssh.com", nil)
			}
		}
	}()
	for nch := range chans {
		if nch.ChannelType() != "direct-tcpip" {
			s.mu.Lock()
			if nch.ChannelType() == "session" {
				s.sessions++
			} else {
				s.other = append(s.other, nch.ChannelType())
			}
			s.mu.Unlock()
			nch.Reject(ssh.UnknownChannelType, "not allowed")
			continue
		}
		var p struct {
			DestAddr string
			DestPort uint32
			OrigAddr string
			OrigPort uint32
		}
		if err := ssh.Unmarshal(nch.ExtraData(), &p); err != nil {
			nch.Reject(ssh.ConnectionFailed, "bad payload")
			continue
		}
		s.mu.Lock()
		s.targets = append(s.targets, net.JoinHostPort(p.DestAddr, strconv.Itoa(int(p.DestPort))))
		s.mu.Unlock()
		up, err := net.Dial("tcp", s.echo.Addr().String())
		if err != nil {
			nch.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			up.Close()
			continue
		}
		go ssh.DiscardRequests(chReqs)
		go func() { io.Copy(ch, up); ch.Close() }()
		go func() { io.Copy(up, ch); up.Close() }()
	}
}

func (s *testSSHServer) requested() (targets []string, sessions int, other []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.targets...), s.sessions, append([]string(nil), s.other...)
}

// clientConfig returns an SSHConfig pointing at the server, using the key
// file path (no agent) and appKnownHosts as the app's known_hosts file.
func (s *testSSHServer) clientConfig(appKnownHosts string) SSHConfig {
	s.t.Setenv("SSH_AUTH_SOCK", "")
	return SSHConfig{
		Host:          s.host,
		Port:          s.port,
		User:          "tester",
		KeyPath:       s.keyPath,
		AgentPort:     s.echoPort(),
		AppKnownHosts: appKnownHosts,
	}
}

func (s *testSSHServer) connCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// encryptKey rewrites the client key file passphrase-protected.
func (s *testSSHServer) encryptKey(pass string) {
	raw, err := os.ReadFile(s.keyPath)
	if err != nil {
		s.t.Fatal(err)
	}
	k, err := ssh.ParseRawPrivateKey(raw)
	if err != nil {
		s.t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(k, "", []byte(pass))
	if err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(s.keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		s.t.Fatal(err)
	}
}

func (s *testSSHServer) swallowKeepalives(v bool) {
	s.mu.Lock()
	s.swallow = v
	s.mu.Unlock()
}
