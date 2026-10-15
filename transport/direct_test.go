package transport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestDirectDialAcceptsLocalListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	d := NewDirect("127.0.0.1", port)
	defer d.Close()

	conn, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatalf("DialAgent: %v", err)
	}
	defer conn.Close()

	select {
	case srv := <-accepted:
		srv.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not accept the dial")
	}
}

func TestDirectDialClosedPortFailsWithin3s(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	d := NewDirect("127.0.0.1", port)
	start := time.Now()
	conn, err := d.DialAgent(context.Background())
	elapsed := time.Since(start)
	if err == nil {
		conn.Close()
		t.Fatal("expected error dialing closed port")
	}
	if elapsed >= 3*time.Second {
		t.Fatalf("dial took %v, want < 3s", elapsed)
	}
}

func TestDirectDialHonoursCancelledContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := NewDirect("127.0.0.1", ln.Addr().(*net.TCPAddr).Port)
	start := time.Now()
	conn, err := d.DialAgent(ctx)
	elapsed := time.Since(start)
	if err == nil {
		conn.Close()
		t.Fatal("expected error from cancelled context")
	}
	if elapsed > time.Second {
		t.Fatalf("cancelled dial took %v, want prompt return", elapsed)
	}
}

func TestDirectCloseReturnsNil(t *testing.T) {
	if err := NewDirect("127.0.0.1", 1).Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestHostKeyErrorUnknown(t *testing.T) {
	var err error = &HostKeyError{Host: "box.local", Fingerprint: "SHA256:abc", Mismatch: false}
	want := "unknown SSH host key for box.local: SHA256:abc"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	var hk *HostKeyError
	if !errors.As(err, &hk) || hk.Mismatch {
		t.Fatal("errors.As should recover *HostKeyError with Mismatch false")
	}
}

func TestHostKeyErrorMismatch(t *testing.T) {
	err := &HostKeyError{Host: "box.local", Fingerprint: "SHA256:abc", Mismatch: true}
	want := "SSH host key for box.local changed: SHA256:abc"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}
