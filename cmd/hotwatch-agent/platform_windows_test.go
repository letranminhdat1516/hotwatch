package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

// A pipe served by an ordinary process (this test, run from a temp folder)
// must not pass for the Docker daemon.
func TestDockerPipeRejectsUntrustedServer(t *testing.T) {
	path := fmt.Sprintf(`\\.\pipe\hotwatch-test-%d`, os.Getpid())
	ln, err := winio.ListenPipe(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c, err := dialDockerPipe(ctx, path); err == nil {
		c.Close()
		t.Fatal("a pipe served from outside Program Files by a non-SYSTEM process must be refused")
	}
}
