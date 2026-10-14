package hub

import (
	"context"
	"sync"
	"time"

	"hotwatch/model"
	"hotwatch/source"
	"hotwatch/transport"
)

// HostView is the copied state the UI renders for one host. Every field is
// safe to read after publish returns: slices and pointers are never mutated
// by the poller afterwards (Hist is internally synchronised).
type HostView struct {
	Info     model.HostInfo
	Conn     Conn
	Snap     *model.Snapshot // last successful snapshot; nil before the first
	Age      time.Duration   // now - time of the last successful snapshot; 0 if none
	Hist     *History
	Procs    []model.Process // last successful fetch; kept when a later fetch fails
	Ctrs     []model.Container
	ProcsErr error                   // last Processes fetch error, nil after a success; never affects Conn
	CtrsErr  error                   // likewise for Containers
	HostKey  *transport.HostKeyError // set in HostKeyUnknown / HostKeyMismatch
}

// Poll periods for hosts that are not the selected one.
const (
	visibleInterval = 15 * time.Second
)

// Pages that need extra data from the selected host.
const (
	PageProcesses = "processes"
	PageDocker    = "docker"
)

// Hub polls every source according to its tier and publishes HostViews.
type Hub struct {
	interval time.Duration
	publish  func(id string, v HostView)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	editMu sync.Mutex // serialises AddHost, RemoveHost and ReplaceSource

	mu       sync.Mutex // guards the fields below and each host's reset/force
	stopped  bool
	selected string
	page     string
	visible  map[string]bool
	hosts    map[string]*host
}

// New starts one poller goroutine per source. publish is called only from the
// poller goroutines (possibly concurrently for different hosts) and must not
// call Stop, AddHost, RemoveHost or ReplaceSource.
func New(sources map[string]source.Source, interval time.Duration, publish func(id string, v HostView)) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{interval: interval, publish: publish, ctx: ctx, cancel: cancel,
		visible: map[string]bool{}, hosts: map[string]*host{}}
	h.mu.Lock()
	for id, src := range sources {
		h.spawnLocked(id, src, NewHistory())
	}
	h.mu.Unlock()
	return h
}

// spawnLocked starts the poller goroutine for id. h.mu must be held and the
// hub not stopped.
func (h *Hub) spawnLocked(id string, src source.Source, hist *History) {
	ctx, cancel := context.WithCancel(h.ctx)
	p := &host{id: id, src: src, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		wake: make(chan struct{}, 1), hist: hist}
	h.hosts[id] = p
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer close(p.done)
		h.run(p)
	}()
}

// AddHost starts polling a new host. An id that already exists, or a stopped
// hub, is ignored.
func (h *Hub) AddHost(id string, src source.Source) {
	h.editMu.Lock()
	defer h.editMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped || h.hosts[id] != nil {
		return
	}
	h.spawnLocked(id, src, NewHistory())
}

// RemoveHost stops polling id and waits for its goroutine to exit; its
// history is dropped. No publish for id happens after it returns.
func (h *Hub) RemoveHost(id string) {
	h.editMu.Lock()
	defer h.editMu.Unlock()
	h.mu.Lock()
	p := h.hosts[id]
	delete(h.hosts, id)
	h.mu.Unlock()
	if p != nil {
		p.cancel()
		<-p.done
	}
}

// ReplaceSource swaps id's source (after an address or token edit). The
// history is kept; the connection restarts at Connecting.
func (h *Hub) ReplaceSource(id string, src source.Source) {
	h.editMu.Lock()
	defer h.editMu.Unlock()
	h.mu.Lock()
	old := h.hosts[id]
	h.mu.Unlock()
	if old == nil {
		return
	}
	old.cancel()
	<-old.done
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.stopped {
		h.spawnLocked(id, src, old.hist)
	}
}

func (h *Hub) wakeAll() {
	h.mu.Lock()
	ps := make([]*host, 0, len(h.hosts))
	for _, p := range h.hosts {
		ps = append(ps, p)
	}
	h.mu.Unlock()
	for _, p := range ps {
		p.poke()
	}
}

// Select makes hostID the polled-at-interval host and page the open page.
func (h *Hub) Select(hostID, page string) {
	h.mu.Lock()
	changed := h.selected != hostID || h.page != page
	h.selected, h.page = hostID, page
	if p := h.hosts[hostID]; p != nil && changed {
		p.force = true
	}
	h.mu.Unlock()
	h.wakeAll()
}

// SetVisible declares which hosts have a panel row on screen.
func (h *Hub) SetVisible(hostIDs []string) {
	h.mu.Lock()
	h.visible = make(map[string]bool, len(hostIDs))
	for _, id := range hostIDs {
		h.visible[id] = true
	}
	h.mu.Unlock()
	h.wakeAll()
}

// Retry moves hostID to Connecting (EvReset) and polls it again.
func (h *Hub) Retry(hostID string) {
	h.mu.Lock()
	p := h.hosts[hostID]
	if p != nil {
		p.reset = true
	}
	h.mu.Unlock()
	if p != nil {
		p.poke()
	}
}

// Stop cancels every poller and waits for them to exit. It is idempotent and
// must not be called from publish.
func (h *Hub) Stop() {
	h.mu.Lock()
	h.stopped = true
	h.mu.Unlock()
	h.cancel()
	h.wg.Wait()
}
