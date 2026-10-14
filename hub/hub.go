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
	Info    model.HostInfo
	Conn    Conn
	Snap    *model.Snapshot // last successful snapshot; nil before the first
	Age     time.Duration   // now - time of the last successful snapshot; 0 if none
	Hist    *History
	Procs   []model.Process
	Ctrs    []model.Container
	HostKey *transport.HostKeyError // set in HostKeyUnknown / HostKeyMismatch
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
	clk      clock

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	selected string
	page     string
	visible  map[string]bool
	hosts    map[string]*host
}

// New starts one poller goroutine per source. publish is called only from the
// poller goroutines (possibly concurrently for different hosts) and must not
// call Stop.
func New(sources map[string]source.Source, interval time.Duration, publish func(id string, v HostView)) *Hub {
	return newHub(sources, interval, publish, realClock{})
}

func newHub(sources map[string]source.Source, interval time.Duration, publish func(id string, v HostView), clk clock) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{interval: interval, publish: publish, clk: clk, ctx: ctx, cancel: cancel,
		visible: map[string]bool{}, hosts: map[string]*host{}}
	for id, src := range sources {
		h.hosts[id] = &host{id: id, src: src, wake: make(chan struct{}, 1)}
	}
	for _, p := range h.hosts {
		h.wg.Add(1)
		go func() { defer h.wg.Done(); h.run(p) }()
	}
	return h
}

func (h *Hub) wakeAll() {
	for _, p := range h.hosts {
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
	h.cancel()
	h.wg.Wait()
}
