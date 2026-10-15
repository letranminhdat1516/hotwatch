package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"hotwatch/hub"
	"hotwatch/mac/appicon"
	"hotwatch/mac/apps"
	"hotwatch/mac/pkgs"
	"hotwatch/mac/symbols"
	"hotwatch/model"
	"hotwatch/source"
	"hotwatch/source/remote"
	"hotwatch/store"
	"hotwatch/transport"
	hwui "hotwatch/ui"
	"hotwatch/ui/hostpanel"
	"hotwatch/ui/pages"
)

const (
	defaultAgentPort = 7171
	defaultInterval  = 2 * time.Second
	columnsDebounce  = 500 * time.Millisecond
)

// paths are the files the app reads and writes.
type paths struct {
	Home           string // for "~/" in key paths
	Config         string
	AppKnownHosts  string // the only known_hosts file the app writes
	UserKnownHosts string // ~/.ssh/known_hosts, read only
}

func appPaths(home string) paths {
	dir := filepath.Join(home, "Library", "Application Support", "Hotwatch")
	return paths{Home: home, Config: filepath.Join(dir, "config.json"),
		AppKnownHosts: filepath.Join(dir, "known_hosts"), UserKnownHosts: filepath.Join(home, ".ssh", "known_hosts")}
}

// sourceEnv is what building a host's source needs besides the host.
type sourceEnv struct {
	Secrets    store.Secrets
	Paths      paths
	Passphrase func(ctx context.Context, keyPath string) (string, error)
	Local      func() source.Source
}

// newSSHDialer is transport.NewSSH; tests replace it.
var newSSHDialer = transport.NewSSH

// failing is a Source whose every call returns err: a host that cannot be
// reached as configured. It never dials.
type failing struct{ err error }

func (f failing) Info(context.Context) (model.HostInfo, error)       { return model.HostInfo{}, f.err }
func (f failing) Snapshot(context.Context) (model.Snapshot, error)   { return model.Snapshot{}, f.err }
func (f failing) Processes(context.Context) ([]model.Process, error) { return nil, f.err }
func (f failing) Signal(context.Context, int32, time.Time, model.Signal) error {
	return f.err
}
func (f failing) Containers(context.Context) ([]model.Container, error) { return nil, f.err }
func (f failing) ContainerAction(context.Context, string, model.ContainerAction) error {
	return f.err
}
func (f failing) ContainerLogs(context.Context, string, int, bool) (<-chan model.LogLine, error) {
	return nil, f.err
}

// pending stands in for an agent host until its token has been read off the
// main thread: polls wait (the host stays Connecting) until ReplaceSource
// cancels them; actions fail at once.
type pending struct{}

var errPending = &model.APIError{Code: "unavailable", Message: "connecting"}

func (pending) Info(ctx context.Context) (model.HostInfo, error) {
	<-ctx.Done()
	return model.HostInfo{}, ctx.Err()
}
func (pending) Snapshot(ctx context.Context) (model.Snapshot, error) {
	<-ctx.Done()
	return model.Snapshot{}, ctx.Err()
}
func (pending) Processes(context.Context) ([]model.Process, error) { return nil, errPending }
func (pending) Signal(context.Context, int32, time.Time, model.Signal) error {
	return errPending
}
func (pending) Containers(context.Context) ([]model.Container, error) { return nil, errPending }
func (pending) ContainerAction(context.Context, string, model.ContainerAction) error {
	return errPending
}
func (pending) ContainerLogs(context.Context, string, int, bool) (<-chan model.LogLine, error) {
	return nil, errPending
}

// buildSource returns the source for h and the dialer to close with it (nil
// for none). A host whose token is missing or unreadable gets a source that
// reports Unauthorized without dialing. An error means h's settings are
// unusable.
func buildSource(h store.Host, env sourceEnv) (source.Source, io.Closer, error) {
	switch h.Kind {
	case "local":
		return env.Local(), nil, nil
	case "agent":
	default:
		return nil, nil, fmt.Errorf("host %s: unknown kind %q", h.ID, h.Kind)
	}
	c := h.Conn
	if c == nil {
		return nil, nil, fmt.Errorf("host %s: no connection settings", h.ID)
	}
	port := c.AgentPort
	if port == 0 {
		port = defaultAgentPort
	}
	var d transport.Dialer
	switch c.Type {
	case "direct":
		if c.Address == "" {
			return nil, nil, fmt.Errorf("host %s: no address", h.ID)
		}
		d = transport.NewDirect(c.Address, port)
	case "ssh":
		if c.SSHHost == "" {
			return nil, nil, fmt.Errorf("host %s: no SSH host", h.ID)
		}
	default:
		return nil, nil, fmt.Errorf("host %s: unknown connection type %q", h.ID, c.Type)
	}
	token, err := env.Secrets.Get(store.TokenKey(h.ID))
	switch {
	case errors.Is(err, store.ErrSecretNotFound):
		return failing{&model.APIError{Code: "unauthorized", Message: "token missing"}}, nil, nil
	case err != nil:
		return failing{&model.APIError{Code: "unauthorized", Message: "token unavailable: " + err.Error()}}, nil, nil
	}
	if d == nil {
		d = newSSHDialer(transport.SSHConfig{Host: c.SSHHost, Port: c.SSHPort, User: c.SSHUser,
			KeyPath: expandHome(c.KeyPath, env.Paths.Home), AgentPort: port,
			KnownHostsFiles: []string{env.Paths.UserKnownHosts, env.Paths.AppKnownHosts},
			AppKnownHosts:   env.Paths.AppKnownHosts, Passphrase: env.Passphrase})
	}
	client := remote.New(d.DialAgent, token)
	ac := &agentCloser{client: client, dialer: d}
	if r, ok := d.(transport.PromptResetter); ok {
		return client, &promptingCloser{ac, r}, nil
	}
	return client, ac, nil
}

// agentCloser retires an agent source: idle HTTP connections, then the dialer.
type agentCloser struct {
	client interface{ CloseIdleConnections() }
	dialer transport.Dialer
}

func (a *agentCloser) Close() error {
	a.client.CloseIdleConnections()
	return a.dialer.Close()
}

// promptingCloser is the closer of a source whose dialer prompts for
// passphrases (SSH): Retry resets its declined prompts.
type promptingCloser struct {
	*agentCloser
	transport.PromptResetter
}

func expandHome(p, home string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok && home != "" {
		return filepath.Join(home, rest)
	}
	return p
}

// registry holds each host's current source and dialer, for the runner, the
// logs opener and cleanup.
type registry struct {
	mu      sync.Mutex
	srcs    map[string]source.Source
	closers map[string]io.Closer
}

// set installs id's source and returns the dialer it replaces.
func (r *registry) set(id string, s source.Source, c io.Closer) io.Closer {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.srcs == nil {
		r.srcs, r.closers = map[string]source.Source{}, map[string]io.Closer{}
	}
	old := r.closers[id]
	r.srcs[id], r.closers[id] = s, c
	return old
}

func (r *registry) remove(id string) io.Closer {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.closers[id]
	delete(r.srcs, id)
	delete(r.closers, id)
	return c
}

func (r *registry) closer(id string) io.Closer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closers[id]
}

func (r *registry) lookup(id string) (source.Source, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.srcs[id]
	return s, ok
}

func (r *registry) closeAll() []io.Closer {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs := slices.Collect(maps.Values(r.closers))
	r.srcs, r.closers = nil, nil
	return cs
}

func closeQuietly(c io.Closer) {
	if c != nil {
		c.Close()
	}
}

// configStore owns the saved configuration. The host panel owns hosts,
// groups and UI state; Settings.Columns belongs to the columns hook.
type configStore struct {
	path     string
	log      *slog.Logger
	debounce time.Duration

	mu      sync.Mutex
	cfg     store.Config
	timer   *time.Timer
	pending bool
	closed  bool // after Close, column changes are ignored
}

func newConfigStore(path string, cfg store.Config, log *slog.Logger) *configStore {
	return &configStore{path: path, cfg: cfg, log: log, debounce: columnsDebounce}
}

func (s *configStore) Config() store.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Save writes c, keeping the store's own columns.
func (s *configStore) Save(c store.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.Settings.Columns = s.cfg.Settings.Columns
	if err := store.Save(s.path, c); err != nil {
		return err
	}
	s.cfg = c
	return nil
}

// SetColumns records a page's visible columns and saves them after a quiet
// period.
func (s *configStore) SetColumns(page string, cols []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	m := maps.Clone(s.cfg.Settings.Columns)
	if m == nil {
		m = map[string][]string{}
	}
	m[page] = slices.Clone(cols)
	s.cfg.Settings.Columns = m
	s.pending = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.debounce, s.Flush)
}

// Close writes a pending column change and ignores later ones.
func (s *configStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
	s.closed = true
}

// Flush writes a pending column change now.
func (s *configStore) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
}

func (s *configStore) flushLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !s.pending {
		return
	}
	s.pending = false
	if err := store.Save(s.path, s.cfg); err != nil {
		s.log.Warn("config.save_columns", "err", err)
	}
}

// serial runs hub edits in order on one goroutine, so the main thread never
// waits for a poller to stop.
type serial struct {
	mu     sync.Mutex
	q      []func()
	wake   chan struct{}
	closed bool
	done   chan struct{}
}

func newSerial() *serial {
	s := &serial{wake: make(chan struct{}, 1), done: make(chan struct{})}
	go s.run()
	return s
}

func (s *serial) run() {
	defer close(s.done)
	for {
		s.mu.Lock()
		q, closed := s.q, s.closed
		s.q = nil
		s.mu.Unlock()
		for _, f := range q {
			f()
		}
		if closed && len(q) == 0 {
			return
		}
		if len(q) == 0 {
			<-s.wake
		}
	}
}

// do queues f; after close it is dropped.
func (s *serial) do(f func()) {
	s.mu.Lock()
	if !s.closed {
		s.q = append(s.q, f)
	}
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// flush waits for everything queued so far.
func (s *serial) flush() {
	ch := make(chan struct{})
	s.do(func() { close(ch) })
	select {
	case <-ch:
	case <-s.done:
	}
}

// close runs what is queued and stops.
func (s *serial) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	<-s.done
}

// timings logs the R25 measurements. Its methods run on the main thread.
type timings struct {
	start    time.Time
	now      func() time.Time
	log      *slog.Logger
	first    bool      // ui.first_data logged
	switchAt time.Time // last host/page change not yet drawn
}

// applied notes a view applied to the app: the first local snapshot logs
// ui.first_data.
func (t *timings) applied(id string, v hub.HostView) {
	if t.first || id != "local" || v.Snap == nil {
		return
	}
	t.first = true
	t.log.Info("ui.first_data", "ms", t.now().Sub(t.start).Milliseconds())
}

// selected notes a host or page change.
func (t *timings) selected() { t.switchAt = t.now() }

// frame notes a frame that started at start; the first one started after a
// change logs ui.switch.
func (t *timings) frame(start time.Time) {
	if t.switchAt.IsZero() || start.Before(t.switchAt) {
		return
	}
	t.log.Info("ui.switch", "ms", t.now().Sub(t.switchAt).Milliseconds())
	t.switchAt = time.Time{}
}

// hubAPI is the part of *hub.Hub the wiring drives.
type hubAPI interface {
	AddHost(id string, src source.Source)
	RemoveHost(id string)
	ReplaceSource(id string, src source.Source)
	Retry(hostID string)
	Select(hostID, page string)
	SetVisible(hostIDs []string)
	SetInterval(d time.Duration)
	Stop()
}

// appUI is the part of *ui.App the wiring drives, on the main thread.
type appUI interface {
	Apply(id string, v hub.HostView)
	ApplyAction(a hub.Action)
	SetHosts(hosts []store.Host)
	SelectHost(id string)
	CloseLogs()
	SetHostPanel(p hwui.HostPanel)
	SetPost(post func(func()))
	SetIconSource(src appicon.Source)
	SetSymbolSource(s symbols.Source)
	SetApps(scanner apps.Scanner, open func(bundle string))
	SetPackages(mgr pkgs.Manager)
	SetLogsOpener(o pages.LogsOpener)
	SetColumnsHook(f func(page string, cols []string))
	SetSavedColumns(page string, cols []string)
	SetSettings(s store.Settings)
	SetSettingsHook(f func(store.Settings))
	SetFrameHook(f func(start time.Time))
	SetNotice(text string)
	AskPassphrase(keyPath string, reply func(pass string, ok bool)) (cancel func())
	SetRetry(f func(hostID string))
}

// core is the running application behind the window: hub, runner, sources
// and configuration. post runs a function on the main thread without
// waiting (win.Update).
type core struct {
	cfg     *configStore
	secrets store.Secrets
	env     sourceEnv
	reg     registry
	queue   *serial
	times   *timings
	post    func(func())
	log     *slog.Logger
	runner  *hub.ActionRunner
	apps    *apps.Darwin // the Apps page's scanner and the uninstall executor's
	pkgs    *pkgs.System // the Packages page's manager and the package executor's
	nook    *nookCtl     // nil until startNook (and in tests)
	hub     hubAPI
	app     appUI
	done    chan struct{}
	stopped sync.Once

	selHost, selPage string // last selection, for the hub once it starts
}

func newCore(cfg store.Config, p paths, secrets store.Secrets, local func() source.Source, post func(func()), log *slog.Logger) *core {
	c := &core{cfg: newConfigStore(p.Config, cfg, log), secrets: secrets, queue: newSerial(), post: post, log: log,
		times: &timings{start: time.Now(), now: time.Now, log: log}, done: make(chan struct{}), selPage: "overview"}
	if len(cfg.Hosts) > 0 {
		c.selHost = cfg.Hosts[0].ID
	}
	c.env = sourceEnv{Secrets: secrets, Paths: p, Passphrase: c.passphrase, Local: local}
	exec := hub.SourceExecutor(c.lookup)
	c.apps = apps.New(nil)
	c.pkgs = pkgs.NewManager(nil, nil)
	c.runner = hub.NewRunner(hub.ComposeExecutors(map[hub.ActionKind]hub.Executor{"signal": exec, "container": exec,
		"uninstall": uninstallExecutor(c.apps), "package": packageExecutor(c.pkgs)}),
		func(a hub.Action) { post(func() { c.app.ApplyAction(a) }) })
	return c
}

func (c *core) lookup(id string) (source.Source, bool) { return c.reg.lookup(id) }

func (c *core) interval() time.Duration {
	if ms := c.cfg.Config().Settings.IntervalMs; ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return defaultInterval
}

// setSettings saves a change from the Settings page (the store keeps its
// own columns) and applies what takes effect at once: the poll interval.
// Network units are applied by the window itself.
func (c *core) setSettings(s store.Settings) {
	cfg := c.cfg.Config()
	cfg.Settings = s
	if err := c.cfg.Save(cfg); err != nil {
		c.log.Warn("settings.save", "err", err)
		return
	}
	if c.hub != nil {
		c.hub.SetInterval(c.interval())
	}
	if c.nook != nil {
		c.nook.setOff(s.NookOff)
	}
}

// onSelect is the App's selection callback.
func (c *core) onSelect(host, page string) {
	c.times.selected()
	c.selHost, c.selPage = host, page
	if c.hub != nil {
		c.hub.Select(host, page)
	}
}

// attach wires app to the core. warn is store.Load's warning, shown once.
func (c *core) attach(app appUI, warn store.Warning) {
	c.app = app
	cfg := c.cfg.Config()
	app.SetHostPanel(hostpanel.New(cfg, c.effects()))
	app.SetPost(c.post)
	app.SetIconSource(appicon.NewDarwin(c.post, 40))
	app.SetSymbolSource(symbols.New())
	app.SetApps(c.apps, c.openBundle)
	app.SetPackages(c.pkgs)
	app.SetLogsOpener(c.openLogs)
	app.SetColumnsHook(c.cfg.SetColumns)
	for page, cols := range cfg.Settings.Columns {
		app.SetSavedColumns(page, cols)
	}
	app.SetSettings(cfg.Settings)
	app.SetSettingsHook(c.setSettings)
	app.SetFrameHook(c.times.frame)
	app.SetRetry(c.retry)
	if warn != "" {
		app.SetNotice(string(warn))
	}
}

// source builds h's source, standing in a failing one for unusable settings.
func (c *core) source(h store.Host) (source.Source, io.Closer) {
	src, cl, err := buildSource(h, c.env)
	if err != nil {
		c.log.Warn("host.source", "host", h.ID, "err", err)
		return failing{err}, nil
	}
	return src, cl
}

// startHub starts polling. Call it on the main thread once the window
// exists. Only This Mac's source is built here; agent hosts start on a
// placeholder and get theirs from the edit queue, so the first frame never
// waits on the Keychain.
func (c *core) startHub() {
	srcs := map[string]source.Source{}
	for _, h := range c.cfg.Config().Hosts {
		src, cl := source.Source(pending{}), io.Closer(nil)
		if h.Kind == "local" {
			src, cl = c.source(h)
		}
		c.reg.set(h.ID, src, cl)
		srcs[h.ID] = src
	}
	c.useHub(hub.New(srcs, c.interval(), c.publish))
	c.buildAgents()
}

// buildAgents queues building every agent host's source.
func (c *core) buildAgents() {
	for _, h := range c.cfg.Config().Hosts {
		if h.Kind != "local" {
			c.rebuild(h.ID)
		}
	}
}

// rebuild queues building id's source from the saved configuration (reading
// its token again) and swapping it in; the old dialer is closed. A host no
// longer configured is left alone.
func (c *core) rebuild(id string) {
	c.queue.do(func() {
		i := slices.IndexFunc(c.cfg.Config().Hosts, func(h store.Host) bool { return h.ID == id })
		if i < 0 {
			return
		}
		src, cl := c.source(c.cfg.Config().Hosts[i])
		old := c.reg.set(id, src, cl)
		c.hub.ReplaceSource(id, src)
		closeQuietly(old)
	})
}

// retry connects to id again, as the user asked. A host whose source could
// not be built (its token was unreadable, a locked Keychain say) gets a new
// one. Any other keeps its dialer, and with it decrypted keys, so a key that
// worked is not asked for again; a passphrase prompt the user declined may be
// asked once more. The reset waits for a connect in flight, so it runs on the
// edit queue.
func (c *core) retry(id string) {
	if _, ok := c.host(id); !ok {
		return
	}
	src, _ := c.lookup(id)
	switch src.(type) {
	case failing, pending:
		c.rebuild(id)
		return
	}
	// ResetPrompts never blocks (a prompt may be open), so neither does Retry.
	if r, ok := c.reg.closer(id).(transport.PromptResetter); ok {
		r.ResetPrompts()
	}
	c.hub.Retry(id)
}

func (c *core) host(id string) (store.Host, bool) {
	hs := c.cfg.Config().Hosts
	i := slices.IndexFunc(hs, func(h store.Host) bool { return h.ID == id })
	if i < 0 {
		return store.Host{}, false
	}
	return hs[i], true
}

// useHub adopts h and tells it the selection and the visible hosts.
func (c *core) useHub(h hubAPI) {
	c.hub = h
	h.Select(c.selHost, c.selPage)
	h.SetVisible(c.hostIDs())
}

// publish is the hub's callback: it only schedules work on the main thread.
func (c *core) publish(id string, v hub.HostView) {
	c.post(func() {
		c.app.Apply(id, v)
		c.times.applied(id, v)
		if c.nook != nil {
			c.nook.observe(id, v.Conn.State, time.Now())
			if id == c.nook.app.Selected() { // the nook shows the selected host only
				c.nook.bar.redraw()
				if c.nook.shown {
					c.nook.island.redraw()
				}
			}
		}
	})
}

func (c *core) hostIDs() []string {
	var ids []string
	for _, h := range c.cfg.Config().Hosts {
		ids = append(ids, h.ID)
	}
	return ids
}

// openLogs is the Docker page's LogsOpener.
func (c *core) openLogs(hostID, ctrID string, tail int, follow bool) (<-chan model.LogLine, func(), error) {
	src, ok := c.lookup(hostID)
	if !ok {
		return nil, nil, &model.APIError{Code: "not_found", Message: "unknown host " + hostID}
	}
	ctx, cancel := context.WithCancel(context.Background())
	lines, err := src.ContainerLogs(ctx, ctrID, tail, follow)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return lines, cancel, nil
}

var errPromptCancelled = errors.New("passphrase entry cancelled")

// passphrase asks the user for an SSH key's passphrase. It runs on a dialer
// goroutine and waits for the prompt the main thread shows. When ctx ends
// (the host was removed or replaced) the prompt is withdrawn.
func (c *core) passphrase(ctx context.Context, keyPath string) (string, error) {
	type answer struct {
		pass string
		ok   bool
	}
	ch := make(chan answer, 1)
	var withdraw func() // main thread only; posts run in order
	c.post(func() { withdraw = c.app.AskPassphrase(keyPath, func(p string, ok bool) { ch <- answer{p, ok} }) })
	select {
	case <-ctx.Done():
		c.post(func() {
			if withdraw != nil {
				withdraw()
			}
		})
		return "", ctx.Err()
	case a := <-ch:
		if !a.ok {
			return "", errPromptCancelled
		}
		return a.pass, nil
	case <-c.done:
		return "", errors.New("app is quitting")
	}
}

// shutdown stops everything; it is idempotent. Call it on the main thread.
func (c *core) shutdown() {
	c.stopped.Do(func() {
		close(c.done)
		if c.app != nil {
			c.app.CloseLogs()
		}
		c.queue.close()
		c.runner.Stop()
		if c.hub != nil {
			c.hub.Stop()
		}
		c.cfg.Close()
		for _, cl := range c.reg.closeAll() {
			closeQuietly(cl)
		}
	})
}

func (c *core) effects() *effects { return &effects{c: c} }

// effects is the host panel's hostpanel.Effects. Its methods run on the main
// thread; hub edits go through the serial queue.
type effects struct{ c *core }

var _ hostpanel.Effects = (*effects)(nil)

func (e *effects) SaveConfig(cfg store.Config) error { return e.c.cfg.Save(cfg) }

func (e *effects) SetToken(hostID, token string) error {
	return e.c.secrets.Set(store.TokenKey(hostID), token)
}

var errAddrChanged = errors.New("host address changed since the key was shown — reconnect and verify again")

// TrustHostKey records the key for the address it was seen at, never for the
// host's current settings: if those changed since, it refuses.
func (e *effects) TrustHostKey(hostID string, hk *transport.HostKeyError) error {
	h, ok := e.c.host(hostID)
	if !ok {
		return fmt.Errorf("unknown host %q", hostID)
	}
	conn := h.Conn
	if conn == nil || conn.Type != "ssh" {
		return fmt.Errorf("host %q does not connect over SSH", hostID)
	}
	host, portS, err := net.SplitHostPort(hk.Addr)
	if err != nil {
		return fmt.Errorf("the host key carries no dialed address: %w", errAddrChanged)
	}
	port, err := strconv.Atoi(portS)
	if err != nil {
		return fmt.Errorf("the host key's address %q has no port: %w", hk.Addr, errAddrChanged)
	}
	cur := conn.SSHPort
	if cur == 0 {
		cur = 22
	}
	if host != conn.SSHHost || port != cur {
		return errAddrChanged
	}
	p := e.c.env.Paths
	if err := transport.TrustHostKey(p.AppKnownHosts, host, port, hk.Key, hk.Fingerprint, p.UserKnownHosts); err != nil {
		return err
	}
	e.c.retry(hostID)
	return nil
}

// hostsChanged shows the saved host list in the app and the hub.
func (e *effects) hostsChanged() {
	e.c.app.SetHosts(e.c.cfg.Config().Hosts)
	e.c.hub.SetVisible(e.c.hostIDs())
}

func (e *effects) HostAdded(h store.Host) {
	c := e.c
	c.queue.do(func() {
		src, cl := c.source(h)
		closeQuietly(c.reg.set(h.ID, src, cl))
		c.hub.AddHost(h.ID, src)
	})
	e.hostsChanged()
}

func (e *effects) HostChanged(h store.Host) {
	c := e.c
	if h.Kind != "local" { // This Mac's settings are only its name
		c.rebuild(h.ID)
	}
	e.hostsChanged()
}

func (e *effects) HostRemoved(id string) {
	c := e.c
	c.queue.do(func() {
		c.hub.RemoveHost(id)
		closeQuietly(c.reg.remove(id))
		if err := c.secrets.Delete(store.TokenKey(id)); err != nil {
			c.log.Warn("host.token_delete", "host", id, "err", err)
		}
	})
	e.hostsChanged()
}

func (e *effects) Select(hostID string) { e.c.app.SelectHost(hostID) }
