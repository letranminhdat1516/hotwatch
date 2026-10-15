package hub

import (
	"context"
	"errors"
	"time"

	"hotwatch/model"
	"hotwatch/source"
	"hotwatch/transport"
)

type tier int

const (
	tierHidden tier = iota
	tierVisible
	tierSelected
)

// procsEvery is how many selected-host polls pass between process fetches
// while the Processes page is not open: the Overview's per-app breakdowns
// stay fresh without a full process scan every interval.
const procsEvery = 3

// host is one source's poller state. reset and force are guarded by Hub.mu;
// everything below them is owned by the host's goroutine.
type host struct {
	id     string
	src    source.Source
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{} // closed when the goroutine has exited
	wake   chan struct{}
	reset  bool
	force  bool

	conn     Conn
	info     model.HostInfo
	haveInfo bool
	snap     *model.Snapshot
	hist     *History
	procs    []model.Process
	ctrs     []model.Container
	procsErr error
	ctrsErr  error
	hostKey  *transport.HostKeyError
	polled   bool
	lastPoll time.Time
	polls    int // successful polls while selected; paces the off-page process refresh
}

func (p *host) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (h *Hub) tierOf(id string) (tier, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case id == h.selected:
		return tierSelected, h.page
	case h.visible[id]:
		return tierVisible, ""
	}
	return tierHidden, ""
}

func terminal(s ConnState) bool {
	switch s {
	case Unauthorized, Incompatible, HostKeyUnknown, HostKeyMismatch:
		return true
	}
	return false
}

// due returns when p should next be polled; ok is false when it should wait
// for a change (Select, SetVisible, Retry) instead.
func (h *Hub) due(p *host, t tier, force bool, now time.Time) (at time.Time, ok bool) {
	c := p.conn
	switch {
	case terminal(c.State):
		return time.Time{}, false
	case t == tierHidden:
		// Hidden hosts are contacted once; Connecting means "not yet".
		return now, c.State == Connecting
	case c.State == Offline:
		return c.NextTry, true
	case !p.polled:
		return now, true
	case force && t == tierSelected:
		return now, true
	}
	period := visibleInterval
	if t == tierSelected {
		period = h.interval
	}
	return p.lastPoll.Add(period), true
}

func (h *Hub) run(p *host) {
	p.conn = Conn{State: Connecting}
	h.emit(p)
	for p.ctx.Err() == nil {
		h.mu.Lock()
		reset, force := p.reset, p.force
		p.reset, p.force = false, false
		h.mu.Unlock()
		if reset {
			p.conn = p.conn.Next(EvReset, time.Now())
			p.hostKey = nil
			p.polled = false
			h.emit(p)
		}
		t, page := h.tierOf(p.id)
		now := time.Now()
		at, ok := h.due(p, t, force, now)
		if !ok {
			select {
			case <-p.wake:
			case <-p.ctx.Done():
			}
			continue
		}
		if wait := at.Sub(now); wait > 0 {
			tm := time.NewTimer(wait)
			select {
			case <-tm.C:
			case <-p.wake:
				// Tier or page changed (or a reset arrived): recompute.
				tm.Stop()
				continue
			case <-p.ctx.Done():
				tm.Stop()
				return
			}
		}
		h.poll(p, t, page)
	}
}

// poll performs one round: Info when needed, Snapshot, then page data for the
// selected host. Failure of the first two feeds the connection state machine.
func (h *Hub) poll(p *host, t tier, page string) {
	p.polled = true
	p.lastPoll = time.Now()

	// Info is (re)fetched on first contact and when coming back from Offline,
	// so an agent upgraded meanwhile is caught before its snapshot is trusted.
	if !p.haveInfo || p.conn.State == Connecting || p.conn.State == Offline {
		info, err := p.src.Info(p.ctx)
		if p.ctx.Err() != nil {
			return
		}
		if err == nil {
			p.info, p.haveInfo = info, true
			if !model.Compatible(info.ProtocolVersion) {
				err = &model.IncompatibleError{Version: info.ProtocolVersion}
			}
		}
		if err != nil {
			h.fail(p, err)
			return
		}
	}

	snap, err := p.src.Snapshot(p.ctx)
	if p.ctx.Err() != nil {
		return
	}
	if err != nil {
		h.fail(p, err)
		return
	}
	now := time.Now()
	if snap.Time.IsZero() {
		snap.Time = now
	}
	p.snap = &snap
	p.hist.Push(snap.Time, snap)
	p.conn = p.conn.Next(EvOK, now)
	p.hostKey = nil

	// Publish the snapshot now; page data may be slow.
	h.emit(p)

	if t != tierSelected {
		return
	}
	p.polls++
	// Processes: every poll on their page; otherwise every procsEvery-th
	// poll for hosts that report them, so the Overview's by-app views have
	// data on every page. Containers only on the Docker page.
	wantProcs := page == PageProcesses || (p.info.Caps.Processes && (p.polls-1)%procsEvery == 0)
	wantCtrs := page == PageDocker
	if !wantProcs && !wantCtrs {
		return
	}
	// Page data is best effort: a failure keeps the previous list, is
	// reported in the view, and never changes the connection state.
	if wantProcs {
		v, err := p.src.Processes(p.ctx)
		if p.ctx.Err() != nil {
			return
		}
		if err == nil {
			p.procs = v
		}
		p.procsErr = err
	}
	if wantCtrs {
		v, err := p.src.Containers(p.ctx)
		if p.ctx.Err() != nil {
			return
		}
		if err == nil {
			p.ctrs = v
		}
		p.ctrsErr = err
	}
	h.emit(p)
}

func (h *Hub) fail(p *host, err error) {
	ev := Classify(err)
	p.conn = p.conn.Next(ev, time.Now())
	switch ev {
	case EvHostKeyUnknown, EvHostKeyMismatch:
		var hk *transport.HostKeyError
		if errors.As(err, &hk) {
			p.hostKey = hk
		}
	}
	h.emit(p)
}

func (h *Hub) emit(p *host) {
	if p.ctx.Err() != nil {
		return
	}
	v := HostView{Info: p.info, Conn: p.conn, Snap: p.snap, Hist: p.hist, Procs: p.procs, Ctrs: p.ctrs, ProcsErr: p.procsErr, CtrsErr: p.ctrsErr, HostKey: p.hostKey}
	if !p.conn.LastOK.IsZero() {
		v.Age = time.Now().Sub(p.conn.LastOK)
	}
	h.publish(p.id, v)
}
