// Package sshtest is an in-process SSH server for tests outside transport: it
// accepts one client key and forwards every direct-tcpip channel to a fixed
// target (an in-process agent, say). Test code only.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Server is a running test SSH server.
type Server struct {
	Host    string
	Port    int
	HostKey ssh.PublicKey
	KeyPath string // client private key file, encrypted with the passphrase given to Start

	cfg    *ssh.ServerConfig
	target string
	ln     net.Listener
	mu     sync.Mutex
	conns  []net.Conn
}

// Start listens on 127.0.0.1 and forwards channels to target. A non-empty
// passphrase encrypts the client key file. The server stops at test cleanup.
func Start(t *testing.T, target, passphrase string) *Server {
	t.Helper()
	_, hpriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hsigner, err := ssh.NewSignerFromKey(hpriv)
	if err != nil {
		t.Fatal(err)
	}
	_, cpriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(cpriv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(cpriv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{HostKey: hsigner.PublicKey(), KeyPath: filepath.Join(t.TempDir(), "id_test"), target: target}
	if err := os.WriteFile(s.KeyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	csigner, err := ssh.NewSignerFromKey(cpriv)
	if err != nil {
		t.Fatal(err)
	}
	accepted := string(csigner.PublicKey().Marshal())
	s.cfg = &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) == accepted {
			return &ssh.Permissions{}, nil
		}
		return nil, fmt.Errorf("unknown key")
	}}
	s.cfg.AddHostKey(hsigner)
	s.ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := s.ln.Addr().(*net.TCPAddr)
	s.Host, s.Port = a.IP.String(), a.Port
	go s.accept()
	t.Cleanup(s.stop)
	return s
}

func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, c)
		s.mu.Unlock()
		go s.serve(c)
	}
}

func (s *Server) stop() {
	s.ln.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
}

func (s *Server) serve(nc net.Conn) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, s.cfg)
	if err != nil {
		nc.Close()
		return
	}
	defer conn.Close()
	go func() {
		for r := range reqs {
			if r.WantReply {
				r.Reply(r.Type == "keepalive@openssh.com", nil)
			}
		}
	}()
	for nch := range chans {
		if nch.ChannelType() != "direct-tcpip" {
			nch.Reject(ssh.UnknownChannelType, "not allowed")
			continue
		}
		up, err := net.Dial("tcp", s.target)
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
