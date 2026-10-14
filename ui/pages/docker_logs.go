package pages

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"hotwatch/model"
	"hotwatch/ui/components"
)

const (
	// maxLogLines is how many log lines the pane keeps; older ones fall off.
	maxLogLines = 5000
	// maxLogText bounds one line. The agent already caps lines; this keeps
	// the pane bounded even if it did not.
	maxLogText = 16 << 10
	// pumpBatch is how many queued lines one trip to the UI thread carries.
	pumpBatch = 256
)

var logTails = []int{100, 500, 1000}

// LogsOpener starts a container's log stream on hostID. cancel ends it and
// must be safe to call once; the channel may or may not close afterwards.
type LogsOpener func(hostID, ctrID string, tail int, follow bool) (lines <-chan model.LogLine, cancel func(), err error)

// logPane is the logs split and its stream. Lines arrive from a reader
// goroutine through post, which runs them on the main thread; the mutex only
// keeps a post that runs inline (tests) from racing the frame. Lines are
// kept in memory alone, never written anywhere.
type logPane struct {
	mu sync.Mutex

	open     bool
	host, id string
	name     string
	tailIdx  int
	follow   bool
	opened   struct {
		tailIdx int
		follow  bool
	}
	err    string
	ended  bool
	gen    uint64 // identifies the current stream; posts of older ones are dropped
	cancel func()
	done   chan struct{}

	buf      []model.LogLine // ring: oldest at start
	start, n int

	list  ui.ListState
	split float32
}

func (p *logPane) len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func (p *logPane) at(i int) model.LogLine {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i < 0 || i >= p.n {
		return model.LogLine{}
	}
	return p.buf[(p.start+i)%len(p.buf)]
}

func (p *logPane) push(l model.LogLine) {
	if p.buf == nil {
		p.buf = make([]model.LogLine, maxLogLines)
	}
	if p.n < len(p.buf) {
		p.buf[(p.start+p.n)%len(p.buf)] = l
		p.n++
		return
	}
	p.buf[p.start] = l
	p.start = (p.start + 1) % len(p.buf)
}

func (p *logPane) clearLines() {
	clear(p.buf)
	p.start, p.n = 0, 0
}

// SetLogsOpener sets how the page reaches container logs. Without one the
// Logs buttons report that logs are unavailable.
func (d *Docker) SetLogsOpener(o LogsOpener) { d.opener = o }

// SetPost sets how the reader goroutine hands work to the main thread
// (win.Update in the app). Without one it runs the work where it is.
func (d *Docker) SetPost(post func(func())) { d.post = post }

// OpenLogs opens the pane on container id of the current host, ending any
// stream it had.
func (d *Docker) OpenLogs(id string) {
	name := id
	for _, c := range d.ctrs {
		if c.ID == id {
			name = c.Name
		}
	}
	d.openLogs(d.host, id, name)
}

// CloseLogs closes the pane and cancels its stream. It is safe to call when
// nothing is open.
func (d *Docker) CloseLogs() {
	p := &d.logs
	d.stopStream()
	p.mu.Lock()
	p.open, p.err, p.ended = false, "", false
	p.clearLines()
	p.mu.Unlock()
}

func (d *Docker) openLogs(host, id, name string) {
	p := &d.logs
	p.mu.Lock()
	p.open, p.host, p.id, p.name = true, host, id, name
	p.mu.Unlock()
	d.startStream()
}

// stopStream cancels the running stream, if any, and drops what it still
// has in flight.
func (d *Docker) stopStream() {
	p := &d.logs
	p.mu.Lock()
	p.gen++
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.mu.Unlock()
	if done != nil {
		close(done)
	}
	if cancel != nil {
		cancel()
	}
}

// startStream (re)opens the stream for the pane's container with its tail
// and follow settings, after cancelling the previous one.
func (d *Docker) startStream() {
	p := &d.logs
	d.stopStream()
	p.mu.Lock()
	p.clearLines()
	p.err, p.ended = "", false
	p.opened.tailIdx, p.opened.follow = p.tailIdx, p.follow
	host, id, tail, follow := p.host, p.id, logTails[p.tailIdx], p.follow
	p.mu.Unlock()
	if d.opener == nil {
		p.mu.Lock()
		p.err = "logs are not available"
		p.mu.Unlock()
		return
	}
	lines, cancel, err := d.opener(host, id, tail, follow)
	if err != nil {
		p.mu.Lock()
		p.err = err.Error()
		p.mu.Unlock()
		return
	}
	done := make(chan struct{})
	p.mu.Lock()
	gen := p.gen
	p.cancel, p.done = cancel, done
	p.mu.Unlock()
	post := d.post
	if post == nil {
		post = func(f func()) { f() }
	}
	go p.pump(gen, lines, done, post)
}

// pump reads lines until the stream closes or the pane stops it, and hands
// them to the main thread in batches. It touches no UI state.
func (p *logPane) pump(gen uint64, lines <-chan model.LogLine, done <-chan struct{}, post func(func())) {
	for {
		var first model.LogLine
		select {
		case <-done:
			return
		case l, ok := <-lines:
			if !ok {
				post(func() { p.finish(gen, nil) })
				return
			}
			first = l
		}
		batch := []model.LogLine{cleanLine(first)}
		closed := false
	drain:
		for len(batch) < pumpBatch {
			select {
			case l, ok := <-lines:
				if !ok {
					closed = true
					break drain
				}
				batch = append(batch, cleanLine(l))
			default:
				break drain
			}
		}
		post(func() { p.finish(gen, batch) })
		if closed {
			post(func() { p.finish(gen, nil) })
			return
		}
	}
}

// finish adds batch to the ring; a nil batch marks the stream as ended.
// Lines of a stream the pane has since left are dropped.
func (p *logPane) finish(gen uint64, batch []model.LogLine) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if gen != p.gen {
		return
	}
	if batch == nil {
		p.ended = true
		return
	}
	for _, l := range batch {
		p.push(l)
	}
}

// cleanLine makes a line safe to show: the agent has sanitised it already,
// but control characters, invalid UTF-8 and runaway lengths are dealt with
// again here.
func cleanLine(l model.LogLine) model.LogLine {
	l.Text = cleanText(l.Text)
	return l
}

func cleanText(s string) string {
	truncated := false
	if len(s) > maxLogText {
		s, truncated = s[:maxLogText], true
	}
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0, r == 0x2028, r == 0x2029:
			return utf8.RuneError
		}
		return r
	}, s)
	if truncated {
		s += "…"
	}
	return s
}

// LogTint is the colour of a log line's text: stderr in the Danger token.
func LogTint(t *ui.Theme, stream string) ui.Color {
	if stream == "stderr" {
		return t.Danger
	}
	return t.Text
}

func clock(t time.Time) string {
	if t.IsZero() {
		return "--:--:--"
	}
	return t.Local().Format("15:04:05")
}

// logsView draws the pane: its controls, then the lines.
func (d *Docker) logsView(c *ui.Context) {
	p := &d.logs
	t := c.Theme()
	p.mu.Lock()
	name, errText, ended := p.name, p.err, p.ended
	tailIdx, follow := p.tailIdx, p.follow
	p.mu.Unlock()

	ui.Column(c).Fill().Gap(components.Grid).Padding(components.Grid, 2*components.Grid).Children(func() {
		ui.Row(c).Gap(2 * components.Grid).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).FontSize(13).FontWeight(600).SingleLine()
			ui.Segmented(c, &tailIdx, "100", "500", "1000")
			ui.Toggle(c, &follow, "Follow")
			if ended {
				ui.Text(c, "Stream ended").FontSize(12).TextColor(t.TextMuted).NoWrap()
			}
			ui.Box(c).Grow(1)
			if ui.Button(c, "Clear").Clicked() {
				p.mu.Lock()
				p.clearLines()
				p.mu.Unlock()
			}
			if ui.Button(c, "Close").Clicked() {
				d.closeRequested = true
			}
		})
		p.mu.Lock()
		changed := tailIdx != p.opened.tailIdx || follow != p.opened.follow
		p.tailIdx, p.follow = tailIdx, follow
		p.mu.Unlock()
		if changed {
			d.reopen = true
		}
		if errText != "" {
			ui.Text(c, "Could not open logs: "+errText).FontSize(13).TextColor(t.Danger)
			return
		}
		n := p.len()
		if n == 0 {
			ui.Text(c, "Waiting for log lines…").FontSize(12).TextColor(t.TextMuted)
			return
		}
		p.list.FollowEnd = follow
		ui.List(c, &p.list, n, func(i int) {
			l := p.at(i)
			ui.Row(c).Gap(2 * components.Grid).Children(func() {
				ui.Text(c, clock(l.Time)).FontSize(12).TextColor(t.TextMuted).NoWrap().Shrink(0).Width(64)
				ui.Text(c, l.Text).FontSize(12).TextColor(LogTint(t, l.Stream)).SingleLine().Grow(1)
			})
		}).Fill()
	})
}
