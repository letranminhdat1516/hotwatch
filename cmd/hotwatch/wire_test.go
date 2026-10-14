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
func (a *fakeApp) AskPassphrase(k string, reply func(string, bool)) {
	a.prompts = append(a.prompts, k)
	if a.answer != nil {
		reply(a.answer(k))
	}
}

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
func (h *fakeHub) Stop() { h.rec("stop", "") }
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
	src, cl, err := buildSource(h, sourceEnv{Secrets: sec, Paths: p, Passphrase: func(string) (string, error) { return "", nil }})
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
	time.Sleep(50 * time.Millisecond)
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
	time.Sleep(150 * time.Millisecond)
	got, _, _ := store.Load(p.Config)
	if !slices.Equal(got.Settings.Columns["processes"], []string{"a", "b"}) {
		t.Fatalf("saved columns %v", got.Settings.Columns)
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
	hk := &transport.HostKeyError{Host: "box.test", Fingerprint: ssh.FingerprintSHA256(key), Key: key}

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
	if !e.has("retry", "h1") {
		t.Fatalf("host not retried: %v", e.hub.got())
	}
	if err := e.w.effects().TrustHostKey("nope", hk); err == nil {
		t.Fatal("unknown host accepted")
	}
}

// --- passphrase, logs, timings, shutdown ---------------------------------------

func TestPassphraseAskedOnTheMainLoop(t *testing.T) {
	l := newLoop(t)
	w := newCore(store.Default(), testPaths(t), store.NewMemSecrets(), localFake, l.post, slog.New(slog.DiscardHandler))
	app := newFakeApp()
	app.answer = func(k string) (string, bool) { return "pw-for-" + filepath.Base(k), true }
	l.do(func() { w.attach(app, "") })
	got, err := w.passphrase("/k/id_x")
	if err != nil || got != "pw-for-id_x" {
		t.Fatalf("passphrase = %q %v", got, err)
	}
	app.answer = func(string) (string, bool) { return "", false }
	if _, err := w.passphrase("/k/id_y"); err == nil {
		t.Fatal("cancelled prompt returned no error")
	}
	// An unanswered prompt ends when the app quits.
	app.answer = nil
	res := make(chan error, 1)
	go func() { _, err := w.passphrase("/k/id_z"); res <- err }()
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
