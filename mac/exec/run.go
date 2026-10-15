// Package exec is the one place the desktop app runs external commands
// (brew, mise, mdls, plutil, defaults). Every call takes an argv list, never
// a shell, and a context so nothing can hang the app.
package exec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	osexec "os/exec"
	"time"
)

// MaxOutput caps what System keeps of a command's output.
const MaxOutput = 8 << 20

// ErrTooLarge is returned by System when a command prints more than
// MaxOutput bytes; the output returned with it is the first MaxOutput.
var ErrTooLarge = errors.New("exec: output too large")

// Runner runs name with args and returns its standard output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// System returns a Runner over os/exec with a per-call timeout on top of
// the caller's context and the output cap.
func System(timeout time.Duration) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := osexec.CommandContext(ctx, name, args...)
		cmd.WaitDelay = time.Second
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if len(out) > MaxOutput {
			return out[:MaxOutput], fmt.Errorf("%w: %s printed more than %d bytes", ErrTooLarge, name, MaxOutput)
		}
		if ctxErr := ctx.Err(); ctxErr != nil && err != nil {
			return out, fmt.Errorf("%s: %w", name, ctxErr)
		}
		if err != nil {
			if msg := bytes.TrimSpace(stderr.Bytes()); len(msg) > 0 {
				return out, fmt.Errorf("%s: %w: %s", name, err, lastLine(msg))
			}
			return out, fmt.Errorf("%s: %w", name, err)
		}
		return out, nil
	}
}

func lastLine(b []byte) []byte {
	if i := bytes.LastIndexByte(bytes.TrimRight(b, "\n"), '\n'); i >= 0 {
		return b[i+1:]
	}
	return b
}

// Streaming starts name with args and delivers its combined stdout and
// stderr line by line (without the newline) until it exits; wait then
// returns the exit error, if any. Cancel ctx to stop the command. Lines
// longer than 64 KB are split.
func Streaming(ctx context.Context, name string, args ...string) (lines <-chan string, wait func() error, err error) {
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	ch := make(chan string, 64)
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		pw.Close()
		done <- err
	}()
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64<<10), 64<<10)
		for sc.Scan() {
			ch <- sc.Text()
		}
		pr.Close()
	}()
	return ch, func() error {
		if err := <-done; err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}, nil
}
