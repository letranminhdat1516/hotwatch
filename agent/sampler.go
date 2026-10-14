package agent

import (
	"context"
	"sync"
	"time"

	"hotwatch/collect"
	"hotwatch/model"
)

const (
	defaultInterval = 2 * time.Second
	defaultIdle     = 30 * time.Second
	sampleTimeout   = 10 * time.Second
	firstGrace      = time.Second
)

// Clock is the time source of the sampler (injectable for tests).
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// SamplerOptions configures a Sampler; zero values take defaults.
type SamplerOptions struct {
	Interval time.Duration // sampling period; default 2 s
	Idle     time.Duration // stop this long after the last request; default 30 s
	Clock    Clock
	// GPU, if set, supplies GPUs for each snapshot. A failure leaves GPUs
	// empty and lists "gpu" in Snapshot.Partial.
	GPU func(ctx context.Context) ([]model.GPU, error)
}

type procReq struct {
	ctx  context.Context
	resp chan procResp
}

type procResp struct {
	procs []model.Process
	err   error
}

// Sampler owns a Collector, which is not safe for concurrent use: a single
// goroutine calls Sample and Processes. The goroutine starts on the first
// request, samples every Interval, and exits Idle after the last request; a
// later request restarts it.
type Sampler struct {
	col collect.Collector
	opt SamplerOptions

	mu        sync.Mutex
	running   bool
	everRan   bool
	lastTouch time.Time
	snap      *model.Snapshot
	err       error
	ready     chan struct{} // closed when snap or err is first set in this run
	readyOpen bool
	done      chan struct{} // closed when the current goroutine exits
	reqs      chan procReq
}

// NewSampler returns a stopped Sampler.
func NewSampler(col collect.Collector, opt SamplerOptions) *Sampler {
	if opt.Interval <= 0 {
		opt.Interval = defaultInterval
	}
	if opt.Idle <= 0 {
		opt.Idle = defaultIdle
	}
	if opt.Clock == nil {
		opt.Clock = realClock{}
	}
	return &Sampler{col: col, opt: opt, reqs: make(chan procReq)}
}

// Running reports whether the sampling goroutine is alive.
func (s *Sampler) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// touch records a request and starts the goroutine if needed. It returns the
// run's ready and done channels.
func (s *Sampler) touch() (ready, done chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastTouch = s.opt.Clock.Now()
	if !s.running {
		s.running = true
		s.snap, s.err = nil, nil
		s.ready, s.readyOpen = make(chan struct{}), true
		s.done = make(chan struct{})
		go s.loop(s.everRan, s.done)
		s.everRan = true
	}
	return s.ready, s.done
}

// Snapshot returns the latest snapshot, starting the sampler if it is not
// running. After a start it waits up to one interval (plus a second of grace)
// for the first snapshot. If the latest sample failed, that error is returned
// rather than stale data.
func (s *Sampler) Snapshot(ctx context.Context) (model.Snapshot, error) {
	ready, _ := s.touch()
	s.mu.Lock()
	have := s.snap != nil || s.err != nil
	s.mu.Unlock()
	if !have {
		select {
		case <-ready:
		case <-ctx.Done():
			return model.Snapshot{}, ctx.Err()
		case <-s.opt.Clock.After(s.opt.Interval + firstGrace):
			return model.Snapshot{}, &model.APIError{Code: "unavailable", Message: "first sample not ready yet; retry"}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return model.Snapshot{}, s.err
	}
	if s.snap == nil { // sampler stopped between wake-up and read
		return model.Snapshot{}, &model.APIError{Code: "unavailable", Message: "sampler restarting; retry"}
	}
	return *s.snap, nil
}

// Processes lists processes on the sampler goroutine, never overlapping a
// Sample call.
func (s *Sampler) Processes(ctx context.Context) ([]model.Process, error) {
	for attempt := 0; attempt < 3; attempt++ {
		_, done := s.touch()
		req := procReq{ctx: ctx, resp: make(chan procResp, 1)}
		select {
		case s.reqs <- req:
		case <-done:
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		select {
		case r := <-req.resp:
			return r.procs, r.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, &model.APIError{Code: "unavailable", Message: "sampler restarting; retry"}
}

func (s *Sampler) loop(prime bool, done chan struct{}) {
	defer close(done)
	clk := s.opt.Clock
	var tick <-chan time.Time
	if prime {
		// After a sleep the collector's previous reading is stale, so rates
		// over the gap would be misleading: take a baseline now and publish
		// the first real sample one interval later.
		s.sample(false)
	} else {
		s.sample(true)
	}
	tick = clk.After(s.opt.Interval)
	idle := clk.After(s.opt.Idle)
	for {
		select {
		case r := <-s.reqs:
			ps, err := s.col.Processes(r.ctx)
			r.resp <- procResp{ps, err}
		case <-tick:
			s.sample(true)
			tick = clk.After(s.opt.Interval)
		case <-idle:
			s.mu.Lock()
			rem := s.lastTouch.Add(s.opt.Idle).Sub(clk.Now())
			if rem <= 0 {
				s.running = false
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
			idle = clk.After(rem)
		}
	}
}

func (s *Sampler) sample(publish bool) {
	ctx, cancel := context.WithTimeout(context.Background(), sampleTimeout)
	defer cancel()
	snap, err := s.col.Sample(ctx)
	if err == nil && s.opt.GPU != nil {
		if gpus, gerr := s.opt.GPU(ctx); gerr != nil {
			snap.GPUs = nil
			snap.Partial = append(append([]string(nil), snap.Partial...), "gpu")
		} else {
			snap.GPUs = gpus
		}
	}
	if !publish {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.snap, s.err = nil, err
	} else {
		s.snap, s.err = &snap, nil
	}
	if s.readyOpen {
		close(s.ready)
		s.readyOpen = false
	}
}
