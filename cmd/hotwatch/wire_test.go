package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"hotwatch/agent"
	"hotwatch/collect"
	"hotwatch/docker"
	"hotwatch/hub"
	"hotwatch/internal/sshtest"
	"hotwatch/model"
	"hotwatch/source"
	"hotwatch/store"
	"hotwatch/transport"
	hwui "hotwatch/ui"
	"hotwatch/ui/pages"
)

const testToken = "0123456789abcdef0123456789abcdef"

// --- test doubles -----------------------------------------------------------

// loop is the test's "main thread": posted functions run in order on one
// goroutine.
type loop struct {
	mu   sync.Mutex
	q    []func()
	wake chan struct{}
	stop chan struct{}
	done chan struct{}
}

func newLoop(t *testing.T) *loop {
	l := &loop{wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(l.done)
		for {
			l.mu.Lock()
			q := l.q
			l.q = nil
			l.mu.Unlock()
			for _, f := range q {
				f()
			}
			select {
			case <-l.wake:
			case <-l.stop:
				return
			}
		}
	}()
	t.Cleanup(func() { close(l.stop); <-l.done })
	return l
}

func (l *loop) post(f func()) {
	l.mu.Lock()
	l.q = append(l.q, f)
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// do runs f on the loop and waits for it.
func (l *loop) do(f func()) {
	ch := make(chan struct{})
	l.post(func() { f(); close(ch) })
	<-ch
}

type fakeApp struct {
	views     map[string]hub.HostView
	actions   []hub.Action
	hosts     [][]string // ids passed to each SetHosts
	selected  []string
	notice    string
	columns   map[string][]string
	panel     hwui.HostPanel
	post      func(func())
	logs      pages.LogsOpener
	colHook   func(string, []string)
	frameHook func(time.Time)
	closed    int
	prompts   []string
	answer    func(keyPath string) (string, bool)
	withdrawn []string
	retry     func(string)

	settings     store.Settings
	settingsHook func(store.Settings)
}

func newFakeApp() *fakeApp {
	return &fakeApp{views: map[string]hub.HostView{}, columns: map[string][]string{}}
}

func (a *fakeApp) Apply(id string, v hub.HostView) { a.views[id] = v }
func (a *fakeApp) ApplyAction(x hub.Action)        { a.actions = append(a.actions, x) }
func (a *fakeApp) SetHosts(hs []store.Host) {
	ids := []string{}
	for _, h := range hs {
		ids = append(ids, h.ID)
	}
	a.hosts = append(a.hosts, ids)
}
func (a *fakeApp) SelectHost(id string)                       { a.selected = append(a.selected, id) }
func (a *fakeApp) CloseLogs()                                 { a.closed++ }
func (a *fakeApp) SetHostPanel(p hwui.HostPanel)              { a.panel = p }
func (a *fakeApp) SetPost(p func(func()))                     { a.post = p }
func (a *fakeApp) SetLogsOpener(o pages.LogsOpener)           { a.logs = o }
func (a *fakeApp) SetColumnsHook(f func(string, []string))    { a.colHook = f }
func (a *fakeApp) SetSavedColumns(page string, cols []string) { a.columns[page] = cols }
func (a *fakeApp) SetFrameHook(f func(time.Time))             { a.frameHook = f }
func (a *fakeApp) SetNotice(s string)                         { a.notice = s }
func (a *fakeApp) SetSettings(s store.Settings)               { a.settings = s }
func (a *fakeApp) SetSettingsHook(f func(store.Settings))     { a.settingsHook = f }
func (a *fakeApp) AskPassphrase(k string, reply func(string, bool)) func() {
	a.prompts = append(a.prompts, k)
	if a.answer != nil {
		reply(a.answer(k))
	}
	return func() { a.withdrawn = append(a.withdrawn, k) }
}
func (a *fakeApp) SetRetry(f func(string)) { a.retry = f }

type hubCall struct{ op, id string }

type fakeHub struct {
	mu      sync.Mutex
	calls   []hubCall
	srcs    map[string]source.Source
	visible []string
}

func (h *fakeHub) rec(op, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, hubCall{op, id})
}
func (h *fakeHub) AddHost(id string, s source.Source) {
	h.rec("add", id)
	h.mu.Lock()
	if h.srcs == nil {
		h.srcs = map[string]source.Source{}
	}
	h.srcs[id] = s
	h.mu.Unlock()
}
func (h *fakeHub) RemoveHost(id string) { h.rec("remove", id) }
func (h *fakeHub) ReplaceSource(id string, s source.Source) {
	h.rec("replace", id)
	h.mu.Lock()
	if h.srcs == nil {
		h.srcs = map[string]source.Source{}
	}
	h.srcs[id] = s
	h.mu.Unlock()
}
func (h *fakeHub) Retry(id string)        { h.rec("retry", id) }
func (h *fakeHub) Select(id, page string) { h.rec("select", id+"/"+page) }
func (h *fakeHub) SetVisible(ids []string) {
	h.mu.Lock()
	h.visible = slices.Clone(ids)
	h.mu.Unlock()
}
func (h *fakeHub) Stop()                        { h.rec("stop", "") }
func (h *fakeHub) SetInterval(d time.Duration) { h.rec("interval", d.String()) }
func (h *fakeHub) got() []hubCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

// fakeDialer counts closes.
type fakeDialer struct{ closed atomic.Int32 }

func (d *fakeDialer) DialAgent(context.Context) (net.Conn, error) { return nil, errors.New("fake") }
func (d *fakeDialer) Close() error                                { d.closed.Add(1); return nil }

// stubSSH replaces the SSH dialer constructor, recording each config.
func stubSSH(t *testing.T) (*[]transport.SSHConfig, *[]*fakeDialer) {
	var cfgs []transport.SSHConfig
	var ds []*fakeDialer
	old := newSSHDialer
	newSSHDialer = func(c transport.SSHConfig) transport.Dialer {
		d := &fakeDialer{}
		cfgs, ds = append(cfgs, c), append(ds, d)
		return d
	}
	t.Cleanup(func() { newSSHDialer = old })
	return &cfgs, &ds
}

func testPaths(t *testing.T) paths {
	dir := t.TempDir()
	return paths{Home: dir, Config: filepath.Join(dir, "support", "config.json"),
		AppKnownHosts: filepath.Join(dir, "support", "known_hosts"), UserKnownHosts: filepath.Join(dir, ".ssh", "known_hosts")}
}

func localFake() source.Source {
	return &source.Fake{InfoV: model.HostInfo{ProtocolVersion: model.ProtocolVersion, Hostname: "mac"}, Snaps: []model.Snapshot{{CPU: model.CPU{Total: 12}}}}
}

func sshHost(id string) store.Host {
	return store.Host{ID: id, Name: id, Kind: "agent", Conn: &store.Conn{Type: "ssh", SSHHost: "box.test", SSHPort: 2222, SSHUser: "me", KeyPath: "~/.ssh/id_test"}}
}

// waitFor polls cond on the loop until it holds.
func waitFor(t *testing.T, l *loop, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var ok bool
		l.do(func() { ok = cond() })
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// --- paths and sources -------------------------------------------------------

func TestAppPaths(t *testing.T) {
	p := appPaths("/Users/me")
	want := paths{Home: "/Users/me",
		Config:         "/Users/me/Library/Application Support/Hotwatch/config.json",
		AppKnownHosts:  "/Users/me/Library/Application Support/Hotwatch/known_hosts",
		UserKnownHosts: "/Users/me/.ssh/known_hosts"}
	if p != want {
		t.Fatalf("appPaths = %+v", p)
	}
}

func TestBuildSourceLocal(t *testing.T) {
	want := localFake()
	src, cl, err := buildSource(store.Default().Hosts[0], sourceEnv{Secrets: store.NewMemSecrets(), Local: func() source.Source { return want }})
	if err != nil || src != want || cl != nil {
		t.Fatalf("buildSource(local) = %v %v %v", src, cl, err)
	}
}

func TestBuildSourceSSH(t *testing.T) {
	cfgs, _ := stubSSH(t)
	p := testPaths(t)
	sec := store.NewMemSecrets()
	sec.Set(store.TokenKey("h1"), testToken)
	h := sshHost("h1")
	src, cl, err := buildSource(h, sourceEnv{Secrets: sec, Paths: p, Passphrase: func(context.Context, string) (string, error) { return "", nil }})
	if err != nil || src == nil || cl == nil {
		t.Fatalf("buildSource(ssh) = %v %v %v", src, cl, err)
	}
	if len(*cfgs) != 1 {
		t.Fatalf("ssh dialers %d", len(*cfgs))
	}
	c := (*cfgs)[0]
	if c.Host != "box.test" || c.Port != 2222 || c.User != "me" || c.KeyPath != filepath.Join(p.Home, ".ssh", "id_test") ||
		c.AgentPort != defaultAgentPort || c.AppKnownHosts != p.AppKnownHosts ||
		!slices.Equal(c.KnownHostsFiles, []string{p.UserKnownHosts, p.AppKnownHosts}) || c.Passphrase == nil {
		t.Fatalf("SSHConfig = %+v", c)
	}
}

func TestBuildSourceRejectsBadConn(t *testing.T) {
	sec := store.NewMemSecrets()
	sec.Set(store.TokenKey("h1"), testToken)
	for _, h := range []store.Host{
		{ID: "h1", Kind: "agent"},
		{ID: "h1", Kind: "agent", Conn: &store.Conn{Type: "carrier-pigeon"}},
		{ID: "h1", Kind: "agent", Conn: &store.Conn{Type: "direct"}},
		{ID: "h1", Kind: "toaster"},
	} {
		if _, _, err := buildSource(h, sourceEnv{Secrets: sec}); err == nil {
			t.Errorf("buildSource(%+v) accepted", h)
		}
	}
}

// A host without a token is Unauthorized ("token missing") and never dials.
func TestMissingTokenIsUnauthorizedWithoutDialing(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepted atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	h := store.Host{ID: "h1", Kind: "agent", Conn: &store.Conn{Type: "direct", Address: "127.0.0.1", AgentPort: port}}
	src, cl, err := buildSource(h, sourceEnv{Secrets: store.NewMemSecrets()})
	if err != nil || cl != nil {
		t.Fatalf("buildSource = %v %v", cl, err)
	}
	_, ierr := src.Info(context.Background())
	_, serr := src.Snapshot(context.Background())
	var api *model.APIError
	if !errors.As(ierr, &api) || api.Message != "token missing" || hub.Classify(ierr) != hub.EvUnauthorized || hub.Classify(serr) != hub.EvUnauthorized {
		t.Fatalf("Info err %v, Snapshot err %v", ierr, serr)
	}
	if n := accepted.Load(); n != 0 {
		t.Fatalf("dialed %d times without a token", n)
	}
}

// --- the remote path end to end -----------------------------------------------

// A direct host wired to an in-process agent reaches Online and its snapshot
// is applied to the app on the main loop.
func TestRemoteHostReachesOnline(t *testing.T) {
	col := &collect.Fake{Snap: model.Snapshot{CPU: model.CPU{Total: 42}}}
	cfg := agent.Config{TokenSHA256: agent.SHA256Hex(testToken), Info: func() model.HostInfo { return model.HostInfo{Hostname: "box"} }}
	srv := httptest.NewServer(agent.New(cfg, col, &docker.FakeEngineAPI{}, &agent.FakeSignaller{}, io.Discard))
	defer srv.Close()
	_, portS, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portS)

	c := store.Default()
	c.Hosts = append(c.Hosts, store.Host{ID: "vps", Name: "VPS", Kind: "agent",
		Conn: &store.Conn{Type: "direct", Address: "127.0.0.1", AgentPort: port}})
	sec := store.NewMemSecrets()
	sec.Set(store.TokenKey("vps"), testToken)

	l := newLoop(t)
	app := newFakeApp()
	w := newCore(c, testPaths(t), sec, localFake, l.post, slog.New(slog.DiscardHandler))
	l.do(func() { w.attach(app, "") })
	l.do(w.startHub)
	defer w.shutdown()

	waitFor(t, l, "vps online with a snapshot", func() bool {
		v := app.views["vps"]
		return v.Conn.State == hub.Online && v.Snap != nil && v.Snap.CPU.Total == 42 && v.Info.Hostname == "box"
	})
	waitFor(t, l, "local snapshot", func() bool { return app.views["local"].Snap != nil })
	if src, ok := w.lookup("vps"); !ok || src == nil {
		t.Fatal("runner lookup misses the remote host")
	}
}

// Publish only schedules: with a main loop that never runs anything, the hub
// keeps polling and stops promptly.
func TestPublishNeverBlocks(t *testing.T) {
	var mu sync.Mutex
	var posted int
	post := func(func()) { mu.Lock(); posted++; mu.Unlock() } // never runs fn
	c := store.Default()
	c.Settings.IntervalMs = 10
	w := newCore(c, testPaths(t), store.NewMemSecrets(), localFake, post, slog.New(slog.DiscardHandler))
	w.attach(newFakeApp(), "")
	w.startHub()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := posted
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d publishes", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	done := make(chan struct{})
	go func() { w.shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown blocked")
	}
}

// --- app configuration --------------------------------------------------------

func TestAttachAppliesConfig(t *testing.T) {
	c := store.Default()
	c.Settings.Columns = map[string][]string{"processes": {"name", "pid"}}
	w := newCore(c, testPaths(t), store.NewMemSecrets(), localFake, func(f func()) { f() }, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	w.attach(app, "config was unreadable")
	if !slices.Equal(app.columns["processes"], []string{"name", "pid"}) {
		t.Fatalf("saved columns not applied: %v", app.columns)
	}
	if app.notice != "config was unreadable" || app.panel == nil || app.post == nil || app.logs == nil ||
		app.colHook == nil || app.frameHook == nil {
		t.Fatalf("app not wired: %+v", app)
	}
	app2 := newFakeApp()
	newCore(store.Default(), testPaths(t), store.NewMemSecrets(), localFake, func(f func()) { f() }, slog.New(slog.DiscardHandler)).attach(app2, "")
	if app2.notice != "" {
		t.Fatalf("notice without a warning: %q", app2.notice)
	}
}

func TestLoadConfigFromDisk(t *testing.T) {
	p := testPaths(t)
	c := store.Default()
	c.Settings.Columns = map[string][]string{"processes": {"cpu"}}
	if err := store.Save(p.Config, c); err != nil {
		t.Fatal(err)
	}
	got, warn, err := store.Load(p.Config)
	if err != nil || warn != "" {
		t.Fatal(err, warn)
	}
	w := newCore(got, p, store.NewMemSecrets(), localFake, func(f func()) { f() }, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	w.attach(app, warn)
	if !slices.Equal(app.columns["processes"], []string{"cpu"}) {
		t.Fatalf("columns %v", app.columns)
	}
	if w.interval() != 2*time.Second {
		t.Fatalf("interval %v", w.interval())
	}
}

func TestIntervalDefaults(t *testing.T) {
	c := store.Default()
	c.Settings.IntervalMs = 0
	w := newCore(c, testPaths(t), store.NewMemSecrets(), localFake, func(f func()) { f() }, slog.New(slog.DiscardHandler))
	if w.interval() != 2*time.Second {
		t.Fatalf("interval %v", w.interval())
	}
	c.Settings.IntervalMs = 5000
	w = newCore(c, testPaths(t), store.NewMemSecrets(), localFake, func(f func()) { f() }, slog.New(slog.DiscardHandler))
	if w.interval() != 5*time.Second {
		t.Fatalf("interval %v", w.interval())
	}
}

// Column changes are saved once after a quiet period, keep the panel's edits,
// and survive the panel's own saves.
func TestColumnsSaveDebounced(t *testing.T) {
	p := testPaths(t)
	cs := newConfigStore(p.Config, store.Default(), slog.New(slog.DiscardHandler))
	cs.debounce = 40 * time.Millisecond
	cs.SetColumns("processes", []string{"a"})
	cs.SetColumns("processes", []string{"a", "b"})
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Fatalf("saved before the quiet period: %v", err)
	}
	var got store.Config
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _, _ = store.Load(p.Config)
		if slices.Equal(got.Settings.Columns["processes"], []string{"a", "b"}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("saved columns %v", got.Settings.Columns)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The panel's copy has stale (nil) columns; the store keeps its own.
	next := store.Default()
	next.Groups = append(next.Groups, store.Group{ID: "g2", Name: "Two"})
	if err := cs.Save(next); err != nil {
		t.Fatal(err)
	}
	got, _, _ = store.Load(p.Config)
	if len(got.Groups) != 2 || !slices.Equal(got.Settings.Columns["processes"], []string{"a", "b"}) {
		t.Fatalf("after panel save %+v", got)
	}
	// A pending change is written by Flush.
	cs.debounce = time.Hour
	cs.SetColumns("processes", []string{"z"})
	cs.Flush()
	got, _, _ = store.Load(p.Config)
	if !slices.Equal(got.Settings.Columns["processes"], []string{"z"}) {
		t.Fatalf("flush did not save: %v", got.Settings.Columns)
	}
}

// --- effects ------------------------------------------------------------------

type effEnv struct {
	w   *core
	hub *fakeHub
	app *fakeApp
	sec *store.MemSecrets
	p   paths
	ds  *[]*fakeDialer
}

func newEffEnv(t *testing.T, c store.Config) *effEnv {
	_, ds := stubSSH(t)
	p := testPaths(t)
	sec := store.NewMemSecrets()
	w := newCore(c, p, sec, localFake, func(f func()) { f() }, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	w.attach(app, "")
	fh := &fakeHub{}
	w.useHub(fh)
	t.Cleanup(w.shutdown)
	return &effEnv{w: w, hub: fh, app: app, sec: sec, p: p, ds: ds}
}

func (e *effEnv) has(op, id string) bool {
	return slices.Contains(e.hub.got(), hubCall{op, id})
}

func TestEffectsAddChangeRemove(t *testing.T) {
	e := newEffEnv(t, store.Default())
	eff := e.w.effects()

	// Add: token first (as the panel does), then config, then the host.
	if err := eff.SetToken("h1", testToken); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.sec.Get(store.TokenKey("h1")); v != testToken {
		t.Fatalf("token not stored: %q", v)
	}
	c := store.Default()
	c.Hosts = append(c.Hosts, sshHost("h1"))
	if err := eff.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := store.Load(e.p.Config); len(got.Hosts) != 2 {
		t.Fatalf("config not saved: %+v", got.Hosts)
	}
	eff.HostAdded(sshHost("h1"))
	e.w.queue.flush()
	if !e.has("add", "h1") {
		t.Fatalf("hub calls %v", e.hub.got())
	}
	if src, ok := e.w.lookup("h1"); !ok || src == nil {
		t.Fatal("added host not in the registry")
	}
	if last := e.app.hosts[len(e.app.hosts)-1]; !slices.Equal(last, []string{"local", "h1"}) {
		t.Fatalf("app hosts %v", e.app.hosts)
	}
	if !slices.Equal(e.hub.visible, []string{"local", "h1"}) {
		t.Fatalf("visible %v", e.hub.visible)
	}

	// Change: a new source replaces the old one, whose dialer is closed.
	changed := sshHost("h1")
	changed.Conn.SSHHost = "other.test"
	eff.HostChanged(changed)
	e.w.queue.flush()
	if !e.has("replace", "h1") || len(*e.ds) != 2 || (*e.ds)[0].closed.Load() != 1 || (*e.ds)[1].closed.Load() != 0 {
		t.Fatalf("change: calls %v, dialers %d", e.hub.got(), len(*e.ds))
	}

	// Remove: hub, token, dialer and app all forget it.
	eff.HostRemoved("h1")
	e.w.queue.flush()
	if !e.has("remove", "h1") {
		t.Fatalf("hub calls %v", e.hub.got())
	}
	if _, err := e.sec.Get(store.TokenKey("h1")); !errors.Is(err, store.ErrSecretNotFound) {
		t.Fatalf("token kept after remove: %v", err)
	}
	if (*e.ds)[1].closed.Load() != 1 {
		t.Fatal("dialer not closed on remove")
	}
	if _, ok := e.w.lookup("h1"); ok {
		t.Fatal("removed host still in the registry")
	}
}

func TestEffectsLocalRenameKeepsSource(t *testing.T) {
	e := newEffEnv(t, store.Default())
	h := store.Default().Hosts[0]
	h.Name = "Studio"
	e.w.effects().HostChanged(h)
	e.w.queue.flush()
	if e.has("replace", "local") {
		t.Fatal("renaming This Mac restarted its collector")
	}
	if len(e.app.hosts) == 0 {
		t.Fatal("app hosts not refreshed")
	}
}

func TestEffectsSelect(t *testing.T) {
	e := newEffEnv(t, store.Default())
	e.w.effects().Select("local")
	if !slices.Equal(e.app.selected, []string{"local"}) {
		t.Fatalf("selected %v", e.app.selected)
	}
}

func TestEffectsTrustHostKey(t *testing.T) {
	c := store.Default()
	c.Hosts = append(c.Hosts, sshHost("h1"))
	e := newEffEnv(t, c)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	key, _ := ssh.NewPublicKey(pub)
	hk := &transport.HostKeyError{Host: "[box.test]:2222", Addr: "box.test:2222", Fingerprint: ssh.FingerprintSHA256(key), Key: key}

	bad := *hk
	bad.Fingerprint = "SHA256:not-it"
	if err := e.w.effects().TrustHostKey("h1", &bad); err == nil || e.has("retry", "h1") {
		t.Fatalf("mismatched fingerprint: err %v, calls %v", err, e.hub.got())
	}
	if err := e.w.effects().TrustHostKey("h1", hk); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(e.p.AppKnownHosts)
	if err != nil || !bytes.Contains(b, []byte("[box.test]:2222")) {
		t.Fatalf("known_hosts %q %v", b, err)
	}
	if _, err := os.Stat(e.p.UserKnownHosts); !os.IsNotExist(err) {
		t.Fatal("~/.ssh/known_hosts was written")
	}
	if lines := bytes.Count(b, []byte("\n")); lines != 1 {
		t.Fatalf("known_hosts has %d lines: %q", lines, b)
	}
	e.w.queue.flush()
	if !e.has("retry", "h1") || e.has("replace", "h1") {
		t.Fatalf("host not retried with its dialer kept: %v", e.hub.got())
	}
	if err := e.w.effects().TrustHostKey("nope", hk); err == nil {
		t.Fatal("unknown host accepted")
	}
}

// The default SSH port is bound too: the entry is the bare host name.
func TestTrustHostKeyDefaultPort(t *testing.T) {
	c := store.Default()
	h := sshHost("h1")
	h.Conn.SSHPort = 0
	c.Hosts = append(c.Hosts, h)
	e := newEffEnv(t, c)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	key, _ := ssh.NewPublicKey(pub)
	hk := &transport.HostKeyError{Host: "box.test", Addr: "box.test:22", Fingerprint: ssh.FingerprintSHA256(key), Key: key}
	if err := e.w.effects().TrustHostKey("h1", hk); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(e.p.AppKnownHosts)
	if !bytes.HasPrefix(b, []byte("box.test ")) {
		t.Fatalf("known_hosts %q", b)
	}
}

// A key shown for one address is never trusted for another: an edit between
// the prompt and Trust refuses, and nothing is written.
func TestTrustHostKeyRefusedAfterAddressEdit(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	key, _ := ssh.NewPublicKey(pub)
	hk := &transport.HostKeyError{Host: "[box.test]:2222", Addr: "box.test:2222", Fingerprint: ssh.FingerprintSHA256(key), Key: key}
	for name, edit := range map[string]func(*store.Conn){
		"host": func(c *store.Conn) { c.SSHHost = "other.test" },
		"port": func(c *store.Conn) { c.SSHPort = 2200 },
	} {
		t.Run(name, func(t *testing.T) {
			c := store.Default()
			c.Hosts = append(c.Hosts, sshHost("h1"))
			e := newEffEnv(t, c)
			edited := sshHost("h1")
			edit(edited.Conn)
			c.Hosts[1] = edited
			if err := e.w.effects().SaveConfig(c); err != nil {
				t.Fatal(err)
			}
			err := e.w.effects().TrustHostKey("h1", hk)
			if err == nil || !strings.Contains(err.Error(), "address changed") {
				t.Fatalf("err = %v", err)
			}
			if _, err := os.Stat(e.p.AppKnownHosts); !os.IsNotExist(err) {
				t.Fatal("known_hosts written for an edited address")
			}
			e.w.queue.flush()
			if e.has("replace", "h1") || e.has("retry", "h1") {
				t.Fatal("host retried after a refused trust")
			}
		})
	}
	// An error without the dialed address cannot be bound: refused.
	c := store.Default()
	c.Hosts = append(c.Hosts, sshHost("h1"))
	e := newEffEnv(t, c)
	noAddr := *hk
	noAddr.Addr = ""
	if err := e.w.effects().TrustHostKey("h1", &noAddr); err == nil {
		t.Fatal("trusted a key without its dialed address")
	}
}

// flakySecrets fails Get until fixed, like a Keychain locked at launch.
type flakySecrets struct {
	*store.MemSecrets
	mu     sync.Mutex
	broken bool
	gate   chan struct{} // non-nil: Get waits for it
	gets   atomic.Int32
}

func (s *flakySecrets) Get(k string) (string, error) {
	s.gets.Add(1)
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	broken := s.broken
	s.mu.Unlock()
	if broken {
		return "", errors.New("keychain locked")
	}
	return s.MemSecrets.Get(k)
}

func isFailing(s source.Source) bool { _, ok := s.(failing); return ok }

// Retry rebuilds the host's source, so a Keychain that was locked is read again.
func TestRetryRereadsKeychain(t *testing.T) {
	_, _ = stubSSH(t)
	c := store.Default()
	c.Hosts = append(c.Hosts, sshHost("h1"))
	sec := &flakySecrets{MemSecrets: store.NewMemSecrets(), broken: true}
	sec.MemSecrets.Set(store.TokenKey("h1"), testToken)
	w := newCore(c, testPaths(t), sec, localFake, func(f func()) { f() }, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	w.attach(app, "")
	fh := &fakeHub{}
	w.useHub(fh)
	t.Cleanup(w.shutdown)
	w.buildAgents()
	w.queue.flush()
	src, _ := w.lookup("h1")
	var api *model.APIError
	if _, err := src.Info(context.Background()); !errors.As(err, &api) || api.Code != "unauthorized" {
		t.Fatalf("locked keychain: %v", err)
	}
	sec.mu.Lock()
	sec.broken = false
	sec.mu.Unlock()
	if app.retry == nil {
		t.Fatal("no Retry hook on the app")
	}
	app.retry("h1")
	w.queue.flush()
	if src, _ := w.lookup("h1"); isFailing(src) {
		t.Fatal("retry kept the failing source")
	}
	if n := slices.Index(fh.got(), hubCall{"replace", "h1"}); n < 0 {
		t.Fatalf("hub calls %v", fh.got())
	}
}

// The first frame never waits on the Keychain: agent hosts start Connecting
// on a placeholder and get their source from the edit queue.
func TestStartHubDoesNotWaitForKeychain(t *testing.T) {
	_, _ = stubSSH(t)
	c := store.Default()
	c.Hosts = append(c.Hosts, sshHost("h1"))
	sec := &flakySecrets{MemSecrets: store.NewMemSecrets(), gate: make(chan struct{})}
	sec.MemSecrets.Set(store.TokenKey("h1"), testToken)
	l := newLoop(t)
	w := newCore(c, testPaths(t), sec, localFake, l.post, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	l.do(func() { w.attach(app, "") })
	started := make(chan struct{})
	go func() { l.do(w.startHub); close(started) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("startHub waited for the Keychain")
	}
	defer w.shutdown()
	waitFor(t, l, "local snapshot while the Keychain is blocked", func() bool { return app.views["local"].Snap != nil })
	var st hub.ConnState
	l.do(func() { st = app.views["h1"].Conn.State })
	if st != hub.Connecting {
		t.Fatalf("h1 state %q while its token is being read", st)
	}
	close(sec.gate)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if src, ok := w.lookup("h1"); ok && !isFailing(src) && !isPending(src) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent source never built")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isPending(s source.Source) bool { _, ok := s.(pending); return ok }

// --- passphrase, logs, timings, shutdown ---------------------------------------

func TestPassphraseAskedOnTheMainLoop(t *testing.T) {
	l := newLoop(t)
	w := newCore(store.Default(), testPaths(t), store.NewMemSecrets(), localFake, l.post, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	app.answer = func(k string) (string, bool) { return "pw-for-" + filepath.Base(k), true }
	l.do(func() { w.attach(app, "") })
	got, err := w.passphrase(context.Background(), "/k/id_x")
	if err != nil || got != "pw-for-id_x" {
		t.Fatalf("passphrase = %q %v", got, err)
	}
	app.answer = func(string) (string, bool) { return "", false }
	if _, err := w.passphrase(context.Background(), "/k/id_y"); err == nil {
		t.Fatal("cancelled prompt returned no error")
	}
	// An unanswered prompt ends when the app quits.
	app.answer = nil
	res := make(chan error, 1)
	go func() { _, err := w.passphrase(context.Background(), "/k/id_z"); res <- err }()
	waitFor(t, l, "third prompt", func() bool { return len(app.prompts) == 3 })
	w.shutdown()
	select {
	case err := <-res:
		if err == nil {
			t.Fatal("prompt after quit returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prompt blocked past shutdown")
	}
}

type logSource struct {
	source.Source
	ctx context.Context
}

func (s *logSource) ContainerLogs(ctx context.Context, id string, tail int, follow bool) (<-chan model.LogLine, error) {
	s.ctx = ctx
	ch := make(chan model.LogLine)
	return ch, nil
}

func TestLogsOpenerCancels(t *testing.T) {
	e := newEffEnv(t, store.Default())
	ls := &logSource{Source: localFake()}
	e.w.reg.set("local", ls, nil)
	_, cancel, err := e.app.logs("local", "web", 100, true)
	if err != nil || ls.ctx == nil {
		t.Fatal(err)
	}
	cancel()
	if ls.ctx.Err() == nil {
		t.Fatal("cancel did not end the stream's context")
	}
	if _, _, err := e.app.logs("ghost", "web", 100, true); err == nil {
		t.Fatal("unknown host opened logs")
	}
}

func TestTimings(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	now := time.Unix(100, 0)
	tm := &timings{start: now, now: func() time.Time { return now }, log: log}

	now = now.Add(250 * time.Millisecond)
	tm.applied("vps", hub.HostView{Snap: &model.Snapshot{}}) // not local
	tm.applied("local", hub.HostView{})                      // no snapshot yet
	tm.applied("local", hub.HostView{Snap: &model.Snapshot{}})
	tm.applied("local", hub.HostView{Snap: &model.Snapshot{}})
	if n := strings.Count(buf.String(), "ui.first_data"); n != 1 || !strings.Contains(buf.String(), "ms=250") {
		t.Fatalf("first_data log %q", buf.String())
	}

	buf.Reset()
	frame := now
	tm.frame(frame) // nothing pending
	now = now.Add(10 * time.Millisecond)
	tm.selected()
	tm.frame(frame) // started before the change: not the switch frame
	now = now.Add(30 * time.Millisecond)
	tm.frame(now)
	tm.frame(now)
	if n := strings.Count(buf.String(), "ui.switch"); n != 1 || !strings.Contains(buf.String(), "ms=30") {
		t.Fatalf("switch log %q", buf.String())
	}
}

func TestShutdownOrder(t *testing.T) {
	e := newEffEnv(t, store.Default())
	c := store.Default()
	c.Hosts = append(c.Hosts, sshHost("h1"))
	e.w.effects().SetToken("h1", testToken)
	e.w.effects().SaveConfig(c)
	e.w.effects().HostAdded(sshHost("h1"))
	e.w.queue.flush()
	e.w.cfg.debounce = time.Hour
	e.w.cfg.SetColumns("processes", []string{"pid"})
	e.w.shutdown()
	e.w.shutdown() // idempotent
	if e.app.closed != 1 || !e.has("stop", "") || (*e.ds)[0].closed.Load() != 1 {
		t.Fatalf("closed logs %d, calls %v", e.app.closed, e.hub.got())
	}
	if got, _, _ := store.Load(e.p.Config); !slices.Equal(got.Settings.Columns["processes"], []string{"pid"}) {
		t.Fatalf("pending columns lost on quit: %v", got.Settings.Columns)
	}
}

// A dial that gives up (host removed or replaced) withdraws its prompt.
func TestPassphraseEndsWithDialCtx(t *testing.T) {
	l := newLoop(t)
	w := newCore(store.Default(), testPaths(t), store.NewMemSecrets(), localFake, l.post, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	l.do(func() { w.attach(app, "") })
	defer w.shutdown()
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { _, err := w.passphrase(ctx, "/k/id_q"); res <- err }()
	waitFor(t, l, "prompt", func() bool { return len(app.prompts) == 1 })
	cancel()
	select {
	case err := <-res:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("prompt outlived its dial")
	}
	waitFor(t, l, "prompt withdrawn", func() bool { return slices.Equal(app.withdrawn, []string{"/k/id_q"}) })
}

func TestSetColumnsAfterShutdownIsIgnored(t *testing.T) {
	e := newEffEnv(t, store.Default())
	e.w.shutdown()
	e.w.cfg.debounce = time.Millisecond
	e.w.cfg.SetColumns("processes", []string{"late"})
	e.w.cfg.mu.Lock()
	armed, pend := e.w.cfg.timer != nil, e.w.cfg.pending
	e.w.cfg.mu.Unlock()
	if armed || pend {
		t.Fatal("column change accepted after shutdown")
	}
	e.w.cfg.Flush()
	if _, err := os.Stat(e.p.Config); !os.IsNotExist(err) {
		t.Fatal("columns saved after shutdown")
	}
}

// --- the real SSH stack ----------------------------------------------------------

type sshStack struct {
	l   *loop
	w   *core
	app *fakeApp
	srv *sshtest.Server
}

// startSSHStack runs the app core against an in-process agent reached through
// an in-process SSH server whose client key is encrypted with "pw". answer
// is the user's reply to prompts (nil: never answers).
func startSSHStack(t *testing.T, answer func(string) (string, bool)) *sshStack {
	t.Setenv("SSH_AUTH_SOCK", "")
	col := &collect.Fake{Snap: model.Snapshot{CPU: model.CPU{Total: 7}}}
	acfg := agent.Config{TokenSHA256: agent.SHA256Hex(testToken)}
	agentSrv := httptest.NewServer(agent.New(acfg, col, &docker.FakeEngineAPI{}, &agent.FakeSignaller{}, io.Discard))
	t.Cleanup(agentSrv.Close)
	srv := sshtest.Start(t, agentSrv.Listener.Addr().String(), "pw")
	p := testPaths(t)
	if err := transport.TrustHostKey(p.AppKnownHosts, srv.Host, srv.Port, srv.HostKey, ssh.FingerprintSHA256(srv.HostKey)); err != nil {
		t.Fatal(err)
	}
	c := store.Default()
	c.Hosts = append(c.Hosts, store.Host{ID: "h1", Name: "box", Kind: "agent", Conn: &store.Conn{Type: "ssh",
		SSHHost: srv.Host, SSHPort: srv.Port, SSHUser: "tester", KeyPath: srv.KeyPath, AgentPort: 7171}})
	sec := store.NewMemSecrets()
	sec.Set(store.TokenKey("h1"), testToken)
	l := newLoop(t)
	w := newCore(c, p, sec, localFake, l.post, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	app.answer = answer
	l.do(func() { w.attach(app, "") })
	l.do(w.startHub)
	t.Cleanup(w.shutdown)
	return &sshStack{l: l, w: w, app: app, srv: srv}
}

// flushWithin fails if the edit queue does not drain in time.
func flushWithin(t *testing.T, w *core) {
	t.Helper()
	done := make(chan struct{})
	go func() { w.queue.flush(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("edit queue blocked")
	}
}

func TestPromptWithdrawnWhenHostRemoved(t *testing.T) {
	st := startSSHStack(t, nil)
	waitFor(t, st.l, "passphrase prompt", func() bool { return len(st.app.prompts) == 1 })
	st.l.do(func() {
		c := st.w.cfg.Config()
		c.Hosts = c.Hosts[:1]
		eff := st.w.effects()
		if err := eff.SaveConfig(c); err != nil {
			t.Error(err)
		}
		eff.HostRemoved("h1")
	})
	flushWithin(t, st.w)
	waitFor(t, st.l, "prompt withdrawn", func() bool {
		return slices.Equal(st.app.withdrawn, []string{st.srv.KeyPath})
	})
}

func TestPromptWithdrawnWhenSourceReplaced(t *testing.T) {
	st := startSSHStack(t, nil)
	waitFor(t, st.l, "passphrase prompt", func() bool { return len(st.app.prompts) == 1 })
	st.l.do(func() {
		h, _ := st.w.host("h1")
		st.w.effects().HostChanged(h)
	})
	flushWithin(t, st.w)
	waitFor(t, st.l, "old prompt withdrawn", func() bool { return len(st.app.withdrawn) == 1 })
	// Only the new dialer's prompt may remain.
	waitFor(t, st.l, "new dialer prompts", func() bool { return len(st.app.prompts) == 2 })
	var open int
	st.l.do(func() { open = len(st.app.prompts) - len(st.app.withdrawn) })
	if open != 1 {
		t.Fatalf("%d prompts open", open)
	}
}

// Retry on a healthy SSH host keeps its dialer and decrypted key: no prompt.
func TestRetryHealthySSHHostDoesNotPromptAgain(t *testing.T) {
	st := startSSHStack(t, func(string) (string, bool) { return "pw", true })
	var lastOK time.Time
	waitFor(t, st.l, "h1 online", func() bool {
		v := st.app.views["h1"]
		lastOK = v.Conn.LastOK
		return v.Conn.State == hub.Online
	})
	st.l.do(func() { st.app.retry("h1") })
	flushWithin(t, st.w)
	waitFor(t, st.l, "h1 online again", func() bool {
		v := st.app.views["h1"]
		return v.Conn.State == hub.Online && v.Conn.LastOK.After(lastOK)
	})
	var n int
	st.l.do(func() { n = len(st.app.prompts) })
	if n != 1 {
		t.Fatalf("asked for the passphrase %d times", n)
	}
}

func TestRetryKeepsHealthySource(t *testing.T) {
	c := store.Default()
	c.Hosts = append(c.Hosts, sshHost("h1"))
	e := newEffEnv(t, c)
	e.sec.Set(store.TokenKey("h1"), testToken)
	e.w.buildAgents()
	e.w.queue.flush()
	e.app.retry("h1")
	e.w.queue.flush()
	replaces := 0
	for _, c := range e.hub.got() {
		if c == (hubCall{"replace", "h1"}) {
			replaces++
		}
	}
	if !e.has("retry", "h1") || replaces != 1 { // the one from buildAgents
		t.Fatalf("calls %v", e.hub.got())
	}
	if len(*e.ds) != 1 || (*e.ds)[0].closed.Load() != 0 {
		t.Fatalf("dialers %d, first closed %d", len(*e.ds), (*e.ds)[0].closed.Load())
	}
}

// A declined passphrase is not asked again on automatic reconnects, but an
// explicit Retry asks exactly once more; the right answer brings the host
// Online.
func TestRetryReasksDeclinedPassphrase(t *testing.T) {
	var mu sync.Mutex
	reply := func(string) (string, bool) { return "", false }
	st := startSSHStack(t, func(k string) (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		return reply(k)
	})
	// The decline fails the connect; wait for a later automatic attempt too.
	waitFor(t, st.l, "a reconnect after the decline", func() bool {
		v := st.app.views["h1"]
		return v.Conn.State != hub.Online && v.Conn.State != hub.Connecting && v.Conn.Failures >= 2
	})
	var n int
	st.l.do(func() { n = len(st.app.prompts) })
	if n != 1 {
		t.Fatalf("automatic reconnects prompted: %d prompts", n)
	}
	mu.Lock()
	reply = func(string) (string, bool) { return "pw", true }
	mu.Unlock()
	st.l.do(func() { st.app.retry("h1") })
	flushWithin(t, st.w)
	waitFor(t, st.l, "h1 online after retry", func() bool { return st.app.views["h1"].Conn.State == hub.Online })
	st.l.do(func() { n = len(st.app.prompts) })
	if n != 2 {
		t.Fatalf("prompts after retry %d, want 2", n)
	}
}

// Retry (and Trust, which uses it) while a passphrase prompt is open and
// unanswered never blocks the edit queue: the prompt stays open and host
// edits still go through.
func TestRetryWithOpenPromptDoesNotBlockQueue(t *testing.T) {
	st := startSSHStack(t, nil)
	waitFor(t, st.l, "passphrase prompt", func() bool { return len(st.app.prompts) == 1 })
	st.l.do(func() { st.app.retry("h1") })
	flushWithin(t, st.w)
	st.l.do(func() {
		if len(st.app.withdrawn) != 0 {
			t.Error("retry withdrew the open prompt")
		}
	})

	h2 := store.Host{ID: "h2", Name: "h2", Kind: "agent", Conn: &store.Conn{Type: "direct", Address: "127.0.0.1", AgentPort: 1}}
	st.l.do(func() {
		eff := st.w.effects()
		if err := eff.SetToken("h2", testToken); err != nil {
			t.Error(err)
		}
		c := st.w.cfg.Config()
		c.Hosts = append(c.Hosts, h2)
		if err := eff.SaveConfig(c); err != nil {
			t.Error(err)
		}
		eff.HostAdded(h2)
	})
	flushWithin(t, st.w)
	if _, ok := st.w.lookup("h2"); !ok {
		t.Fatal("host added after retry is not registered")
	}
	st.l.do(func() {
		c := st.w.cfg.Config()
		c.Hosts = slices.DeleteFunc(c.Hosts, func(h store.Host) bool { return h.ID == "h1" })
		eff := st.w.effects()
		if err := eff.SaveConfig(c); err != nil {
			t.Error(err)
		}
		eff.HostRemoved("h1")
	})
	flushWithin(t, st.w)
	waitFor(t, st.l, "prompt withdrawn", func() bool {
		return slices.Equal(st.app.withdrawn, []string{st.srv.KeyPath})
	})
}

// Retry on a direct host goes straight to the hub, even with the edit queue busy.
func TestRetryDirectHostBypassesQueue(t *testing.T) {
	c := store.Default()
	c.Hosts = append(c.Hosts, store.Host{ID: "hd", Kind: "agent", Conn: &store.Conn{Type: "direct", Address: "127.0.0.1", AgentPort: 1}})
	e := newEffEnv(t, c)
	e.sec.Set(store.TokenKey("hd"), testToken)
	e.w.buildAgents()
	e.w.queue.flush()
	release := make(chan struct{})
	e.w.queue.do(func() { <-release })
	defer close(release)
	e.app.retry("hd")
	if !e.has("retry", "hd") {
		t.Fatalf("retry waited for the edit queue: calls %v", e.hub.got())
	}
}
