package docker

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"hotwatch/model"
)

// chunkReader returns at most n bytes per Read, to split frames across reads.
type chunkReader struct {
	b []byte
	n int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.b) == 0 {
		return 0, io.EOF
	}
	n := min(c.n, len(p), len(c.b))
	copy(p, c.b[:n])
	c.b = c.b[n:]
	return n, nil
}

func chunked(b []byte, n int) io.Reader { return &chunkReader{b: b, n: n} }

// frames builds multiplexed frames from (stream, payload) pairs.
func frames(args ...any) []byte {
	var out []byte
	for i := 0; i < len(args); i += 2 {
		out = append(out, LogFrame(byte(args[i].(int)), args[i+1].(string))...)
	}
	return out
}

func collect(t *testing.T, ch <-chan model.LogLine) []model.LogLine {
	t.Helper()
	var out []model.LogLine
	timeout := time.After(10 * time.Second)
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, l)
		case <-timeout:
			t.Fatal("timed out collecting lines")
		}
	}
}

func sameText(got, want []model.LogLine) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Stream != want[i].Stream || got[i].Text != want[i].Text {
			return false
		}
	}
	return true
}

func TestLogDemuxSplitFrames(t *testing.T) {
	r := chunked(frames(1, "hello\n", 2, "\x1b[31mred\x1b[0m\n"), 3)
	lines := collect(t, demux(r))
	want := []model.LogLine{{Stream: "stdout", Text: "hello"}, {Stream: "stderr", Text: "red"}}
	if !sameText(lines, want) {
		t.Fatalf("got %+v", lines)
	}
}

func TestLogLineCap(t *testing.T) {
	big := strings.Repeat("a", 16<<20) + "\n"
	lines := collect(t, demux(bytes.NewReader(frames(1, big))))
	if len(lines) != 1 || len(lines[0].Text) != 16<<10+len("…") {
		t.Fatalf("lines = %d", len(lines))
	}
	if !strings.HasSuffix(lines[0].Text, "…") {
		t.Fatal("missing truncation marker")
	}
}

func TestLogLineCapKeepsValidUTF8(t *testing.T) {
	// 3-byte runes: the 16 KB cut falls mid-rune.
	big := strings.Repeat("€", 20000) + "\nnext\n"
	lines := collect(t, demux(bytes.NewReader(frames(1, big))))
	if len(lines) != 2 || lines[1].Text != "next" {
		t.Fatalf("lines %d", len(lines))
	}
	if strings.ContainsRune(lines[0].Text, '�') || len(lines[0].Text) > 16<<10+len("…") {
		t.Fatalf("bad capped line (len %d)", len(lines[0].Text))
	}
}

func TestLogLineSpansFramesAndStreams(t *testing.T) {
	// A line split across two stdout frames, interleaved with stderr.
	r := frames(1, "par", 2, "err\n", 1, "tial\nlast-without-newline")
	lines := collect(t, demux(bytes.NewReader(r)))
	want := []model.LogLine{
		{Stream: "stderr", Text: "err"},
		{Stream: "stdout", Text: "partial"},
		{Stream: "stdout", Text: "last-without-newline"},
	}
	if !sameText(lines, want) {
		t.Fatalf("got %+v", lines)
	}
}

func TestLogSanitize(t *testing.T) {
	in := "a\tb\x00c\x07d\r\x1b[1;32mgreen\x1b[0m\x1b]0;title\x07e\x1b[?25lf\xffg\u0085h\n"
	lines := collect(t, demux(bytes.NewReader(frames(1, in))))
	if len(lines) != 1 || lines[0].Text != "a\tbcdgreenef�gh" {
		t.Fatalf("got %q", lines[0].Text)
	}
}

func TestLogTimestampParsed(t *testing.T) {
	in := "2026-10-15T08:30:00.123456789Z hello world\n2026-10-15T08:30:01Z \n"
	lines := collect(t, demux(bytes.NewReader(frames(1, in))))
	if len(lines) != 2 || lines[0].Text != "hello world" || lines[1].Text != "" {
		t.Fatalf("got %+v", lines)
	}
	want := time.Date(2026, 10, 15, 8, 30, 0, 123456789, time.UTC)
	if !lines[0].Time.Equal(want) {
		t.Fatalf("time %v", lines[0].Time)
	}
}

func TestLogRawTTYStream(t *testing.T) {
	in := "2026-10-15T08:30:00Z one\r\n2026-10-15T08:30:01Z \x1b[31mtwo\x1b[0m\r\n"
	lines := collect(t, demuxStream(context.Background(), chunked([]byte(in), 5), true))
	want := []model.LogLine{{Stream: "stdout", Text: "one"}, {Stream: "stdout", Text: "two"}}
	if !sameText(lines, want) || lines[1].Time.IsZero() {
		t.Fatalf("got %+v", lines)
	}
}

func TestLogTruncatedStreamFlushes(t *testing.T) {
	b := frames(1, "complete\n", 1, "cut")
	b = b[:len(b)-1] // frame payload cut short
	lines := collect(t, demux(bytes.NewReader(b)))
	if len(lines) != 2 || lines[0].Text != "complete" || lines[1].Text != "cu" {
		t.Fatalf("got %+v", lines)
	}
}

func TestLogDemuxStopsOnContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	ch := demuxStream(ctx, pr, false)
	go pw.Write(frames(1, "a\nb\n"))
	<-ch // consume one line, leave the sender blocked on the second
	cancel()
	pr.Close()
	select {
	case <-time.After(5 * time.Second):
		t.Fatal("channel not closed after cancel")
	case <-func() chan struct{} {
		done := make(chan struct{})
		go func() {
			for range ch {
			}
			close(done)
		}()
		return done
	}():
	}
}
