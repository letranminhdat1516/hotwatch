package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"hotwatch/model"
)

const (
	// maxLogLine caps the text of one log line; longer lines are cut and end with "…".
	maxLogLine = 16 << 10
	// maxTimestamp bounds the RFC3339Nano prefix Docker adds with timestamps=1.
	maxTimestamp = 64
	truncMarker  = "…"
)

// demux splits a Docker multiplexed log stream into lines.
func demux(r io.Reader) <-chan model.LogLine {
	return demuxStream(context.Background(), r, false)
}

// demuxStream parses r into lines on the returned channel, which closes at
// end of stream or when ctx is done; r is closed then if it is an io.Closer.
// With raw set, r is an unmultiplexed
// (TTY) stream and every line is reported as stdout.
func demuxStream(ctx context.Context, r io.Reader, raw bool) <-chan model.LogLine {
	ch := make(chan model.LogLine)
	go func() {
		defer close(ch)
		if c, ok := r.(io.Closer); ok {
			defer c.Close()
		}
		emit := func(l model.LogLine) bool {
			select {
			case ch <- l:
				return true
			case <-ctx.Done():
				return false
			}
		}
		asm := map[string]*lineAssembler{"stdout": {stream: "stdout"}, "stderr": {stream: "stderr"}}
		flush := func() {
			for _, name := range []string{"stdout", "stderr"} {
				if l, ok := asm[name].finish(); ok && !emit(l) {
					return
				}
			}
		}
		if raw {
			ok := pump(r, asm["stdout"], emit)
			if ok {
				flush()
			}
			return
		}
		var hdr [8]byte
		buf := make([]byte, 32<<10)
		for {
			if _, err := io.ReadFull(r, hdr[:]); err != nil {
				break
			}
			a := asm["stdout"]
			if hdr[0] == 2 {
				a = asm["stderr"]
			}
			remaining := int64(binary.BigEndian.Uint32(hdr[4:]))
			for remaining > 0 {
				n, err := r.Read(buf[:min(int64(len(buf)), remaining)])
				if n > 0 {
					remaining -= int64(n)
					if !a.write(buf[:n], emit) {
						return
					}
				}
				if err != nil {
					break
				}
			}
			if remaining > 0 {
				break
			}
		}
		flush()
	}()
	return ch
}

// pump copies an unmultiplexed stream into a. It reports false when emit refused a line.
func pump(r io.Reader, a *lineAssembler, emit func(model.LogLine) bool) bool {
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 && !a.write(buf[:n], emit) {
			return false
		}
		if err != nil {
			return true
		}
	}
}

// lineAssembler accumulates one stream's bytes into lines, holding at most
// maxLogLine+maxTimestamp bytes per line however long the line is.
type lineAssembler struct {
	stream    string
	buf       []byte
	truncated bool
}

// write consumes p and calls emit for every completed line. It returns false
// when emit does.
func (a *lineAssembler) write(p []byte, emit func(model.LogLine) bool) bool {
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		a.append(chunk)
		if i < 0 {
			return true
		}
		p = p[i+1:]
		l, _ := a.finish()
		if !emit(l) {
			return false
		}
	}
	return true
}

func (a *lineAssembler) append(b []byte) {
	room := maxLogLine + maxTimestamp - len(a.buf)
	if len(b) > room {
		b, a.truncated = b[:room], true
	}
	a.buf = append(a.buf, b...)
}

// finish returns the pending line, if any, and resets the assembler.
func (a *lineAssembler) finish() (model.LogLine, bool) {
	if len(a.buf) == 0 && !a.truncated {
		return model.LogLine{}, false
	}
	raw, truncated := a.buf, a.truncated
	a.buf, a.truncated = a.buf[:0], false

	var ts time.Time
	if sp := bytes.IndexByte(raw, ' '); sp > 0 && sp <= maxTimestamp {
		if t, err := time.Parse(time.RFC3339Nano, string(raw[:sp])); err == nil {
			ts, raw = t, raw[sp+1:]
		}
	}
	if len(raw) > maxLogLine {
		raw, truncated = raw[:maxLogLine], true
	}
	if truncated {
		raw = trimPartialRune(raw)
	}
	text := sanitize(string(raw))
	if truncated {
		text += truncMarker
	}
	return model.LogLine{Time: ts, Stream: a.stream, Text: text}, true
}

// trimPartialRune drops an incomplete UTF-8 sequence at the end of b.
func trimPartialRune(b []byte) []byte {
	for i := 1; i <= utf8.UTFMax-1 && i <= len(b); i++ {
		c := b[len(b)-i]
		if utf8.RuneStart(c) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			break
		}
	}
	return b
}

// sanitize replaces invalid UTF-8, removes ANSI escape sequences and drops
// control characters other than tab.
func sanitize(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == '\t' || !unicode.IsControl(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// skipEscape returns the index after the escape sequence starting at s[i] == ESC.
func skipEscape(s string, i int) int {
	i++
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[': // CSI: parameters 0x30-0x3F, intermediates 0x20-0x2F, final 0x40-0x7E
		i++
		for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3F {
			i++
		}
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2F {
			i++
		}
		if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7E {
			i++
		}
	case ']': // OSC: terminated by BEL or ESC \
		i++
		for i < len(s) {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b {
				if i+1 < len(s) && s[i+1] == '\\' {
					return i + 2
				}
				return i
			}
			i++
		}
	default: // two-byte escape
		i++
	}
	return i
}
