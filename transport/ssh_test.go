package transport

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

func echo(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	n := 0
	for n < len(buf) {
		m, err := c.Read(buf[n:])
		if err != nil {
			t.Fatalf("read after %d bytes: %v", n, err)
		}
		n += m
	}
	if string(buf) != msg {
		t.Fatalf("echo = %q, want %q", buf, msg)
	}
}

func TestUnknownThenTrustedHostKey(t *testing.T) {
	srv := startTestSSHServer(t)
	dir := t.TempDir()
	cfg := srv.clientConfig(filepath.Join(dir, "sub", "known_hosts"))
	d := NewSSH(cfg)
	defer d.Close()
	_, err := d.DialAgent(context.Background())
	var hk *HostKeyError
	if !errors.As(err, &hk) || hk.Mismatch {
		t.Fatalf("want unknown host key, got %v", err)
	}
	if want := ssh.FingerprintSHA256(srv.hostKey); hk.Fingerprint != want || !strings.HasPrefix(hk.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q, want %q", hk.Fingerprint, want)
	}
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, ssh.FingerprintSHA256(srv.hostKey)); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(cfg.AppKnownHosts)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("app known_hosts stat %v err %v", st, err)
	}
	if st, _ := os.Stat(filepath.Dir(cfg.AppKnownHosts)); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode().Perm())
	}
	d2 := NewSSH(cfg)
	defer d2.Close()
	conn, err := d2.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	echo(t, conn, "ping")
}

func TestChangedHostKeyIsMismatch(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := srv.clientConfig(filepath.Join(t.TempDir(), "known_hosts"))
	other := newTestSigner(t).PublicKey()
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, other, ssh.FingerprintSHA256(other)); err != nil {
		t.Fatal(err)
	}
	d := NewSSH(cfg)
	defer d.Close()
	_, err := d.DialAgent(context.Background())
	var hk *HostKeyError
	if !errors.As(err, &hk) || !hk.Mismatch {
		t.Fatalf("want mismatch, got %v", err)
	}
	if hk.Fingerprint != ssh.FingerprintSHA256(srv.hostKey) {
		t.Fatalf("fingerprint %q is not the presented key's", hk.Fingerprint)
	}
}

func TestOnlyLoopbackForwarding(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := srv.clientConfig(filepath.Join(t.TempDir(), "known_hosts"))
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, ssh.FingerprintSHA256(srv.hostKey)); err != nil {
		t.Fatal(err)
	}
	d := NewSSH(cfg)
	defer d.Close()
	for i := 0; i < 2; i++ {
		c, err := d.DialAgent(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		echo(t, c, "x")
		c.Close()
	}
	targets, sessions, other := srv.requested()
	if len(targets) != 2 {
		t.Fatalf("targets = %v", targets)
	}
	want := net.JoinHostPort("127.0.0.1", itoa(cfg.AgentPort))
	for _, tg := range targets {
		if tg != want {
			t.Fatalf("target %q, want %q", tg, want)
		}
	}
	if sessions != 0 || len(other) != 0 {
		t.Fatalf("unexpected channels: sessions=%d other=%v", sessions, other)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestNonDefaultPortMatchesBracketedEntry(t *testing.T) {
	srv := startTestSSHServer(t)
	if srv.port == 22 {
		t.Skip("port 22")
	}
	dir := t.TempDir()
	cfg := srv.clientConfig(filepath.Join(dir, "app_known_hosts"))
	// a user's own file written the way OpenSSH writes non-22 ports
	user := filepath.Join(dir, "known_hosts")
	line := knownhosts.Line([]string{"[" + srv.host + "]:" + itoa(srv.port)}, srv.hostKey) + "\n"
	if err := os.WriteFile(user, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.KnownHostsFiles = []string{user}
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	// and TrustHostKey writes the same bracketed form
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, ssh.FingerprintSHA256(srv.hostKey)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg.AppKnownHosts)
	if !strings.HasPrefix(string(b), "["+srv.host+"]:"+itoa(srv.port)+" ") {
		t.Fatalf("app file line = %q", b)
	}
}

func TestMalformedKnownHostsLineSkippedAndMissingFileOK(t *testing.T) {
	srv := startTestSSHServer(t)
	dir := t.TempDir()
	cfg := srv.clientConfig(filepath.Join(dir, "app"))
	user := filepath.Join(dir, "known_hosts")
	good := knownhosts.Line([]string{knownhosts.Normalize(net.JoinHostPort(srv.host, itoa(srv.port)))}, srv.hostKey)
	content := "this is not a valid line\n@@@ garbage ssh-ed25519 !!!\n" + good + "\n"
	if err := os.WriteFile(user, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.KnownHostsFiles = []string{filepath.Join(dir, "does-not-exist"), user}
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestRedialAfterServerRestart(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := srv.clientConfig(filepath.Join(t.TempDir(), "known_hosts"))
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, ssh.FingerprintSHA256(srv.hostKey)); err != nil {
		t.Fatal(err)
	}
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	echo(t, c, "one")
	c.Close()

	srv.restart()

	c, err = d.DialAgent(context.Background())
	if err != nil {
		t.Fatalf("redial: %v", err)
	}
	defer c.Close()
	echo(t, c, "two")
}

func TestCloseTwiceAndDialAfterClose(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := srv.clientConfig(filepath.Join(t.TempDir(), "known_hosts"))
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, ssh.FingerprintSHA256(srv.hostKey)); err != nil {
		t.Fatal(err)
	}
	d := NewSSH(cfg)
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DialAgent(context.Background()); err == nil {
		t.Fatal("DialAgent after Close should fail")
	}
}

func TestConcurrentDialAgent(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := srv.clientConfig(filepath.Join(t.TempDir(), "known_hosts"))
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, ssh.FingerprintSHA256(srv.hostKey)); err != nil {
		t.Fatal(err)
	}
	d := NewSSH(cfg)
	defer d.Close()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := d.DialAgent(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			defer c.Close()
			echo(t, c, "hello")
		}()
	}
	wg.Wait()
	if conns := srv.connCount(); conns != 1 {
		t.Fatalf("ssh connections = %d, want 1 (client reused)", conns)
	}
}

func TestPassphraseCallbackForEncryptedKey(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := srv.clientConfig(filepath.Join(t.TempDir(), "known_hosts"))
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, ssh.FingerprintSHA256(srv.hostKey)); err != nil {
		t.Fatal(err)
	}
	srv.encryptKey("s3cret")
	// nil callback: key skipped, auth fails, no panic
	d := NewSSH(cfg)
	if _, err := d.DialAgent(context.Background()); err == nil {
		t.Fatal("expected auth failure without passphrase callback")
	}
	d.Close()
	asked := ""
	cfg.Passphrase = func(p string) (string, error) { asked = p; return "s3cret", nil }
	d = NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if asked != srv.keyPath {
		t.Fatalf("asked for %q", asked)
	}
}

func TestKeepaliveClosesClientAfterMissedReplies(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := srv.clientConfig(filepath.Join(t.TempDir(), "known_hosts"))
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, ssh.FingerprintSHA256(srv.hostKey)); err != nil {
		t.Fatal(err)
	}
	d := NewSSH(cfg).(*sshDialer)
	d.keepaliveEvery = 50 * time.Millisecond
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	s := d.current()
	srv.swallowKeepalives(true)
	done := make(chan struct{})
	go func() { s.client.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client not closed after missed keepalives")
	}
}

func TestTrustRefusals(t *testing.T) {
	key := newTestSigner(t).PublicKey()
	other := newTestSigner(t).PublicKey()
	fp := ssh.FingerprintSHA256(key)
	dir := t.TempDir()
	app := filepath.Join(dir, "app", "known_hosts")

	if err := TrustHostKey(app, "h.example", 22, key, ssh.FingerprintSHA256(other)); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("fingerprint mismatch: %v", err)
	}
	if _, err := os.Stat(app); err == nil {
		t.Fatal("file written despite refusal")
	}
	if err := TrustHostKey(app, "h.example", 22, key, fp); err != nil {
		t.Fatal(err)
	}
	if err := TrustHostKey(app, "h.example", 22, key, fp); err != nil { // idempotent
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(app); strings.Count(string(b), "\n") != 1 {
		t.Fatalf("duplicate line: %q", b)
	}
	// a different key (any type) for the same host: hard stop
	if err := TrustHostKey(app, "h.example", 22, other, ssh.FingerprintSHA256(other)); !errors.Is(err, ErrHostKeyConflict) {
		t.Fatalf("conflict in app file: %v", err)
	}
	// ...also when it is in another known_hosts file
	user := filepath.Join(dir, "user_known_hosts")
	os.WriteFile(user, []byte(knownhosts.Line([]string{"other.example"}, key)+"\n"), 0o600)
	if err := TrustHostKey(app, "other.example", 22, other, ssh.FingerprintSHA256(other), user); !errors.Is(err, ErrHostKeyConflict) {
		t.Fatalf("conflict in user file: %v", err)
	}
}

func TestTrustRejectsPatternHosts(t *testing.T) {
	key := newTestSigner(t).PublicKey()
	app := filepath.Join(t.TempDir(), "kh")
	for _, h := range []string{"*", "a*", "a?b", "!a", "|1|x|y", "a,b", "a b", "a\nb", "[a]", ""} {
		if err := TrustHostKey(app, h, 22, key, ssh.FingerprintSHA256(key)); err == nil {
			t.Errorf("host %q accepted", h)
		}
	}
	if _, err := os.Stat(app); err == nil {
		t.Fatal("file created")
	}
}

func TestTrustEnforcesModesAndRefusesSymlink(t *testing.T) {
	key := newTestSigner(t).PublicKey()
	fp := ssh.FingerprintSHA256(key)
	dir := t.TempDir()
	sub := filepath.Join(dir, "d")
	os.Mkdir(sub, 0o755)
	app := filepath.Join(sub, "kh")
	os.WriteFile(app, nil, 0o644)
	if err := TrustHostKey(app, "h.example", 22, key, fp); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(app); st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(sub); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode().Perm())
	}
	target := filepath.Join(dir, "victim")
	os.WriteFile(target, []byte("keep\n"), 0o600)
	link := filepath.Join(sub, "link")
	os.Symlink(target, link)
	if err := TrustHostKey(link, "h2.example", 22, key, fp); err == nil {
		t.Fatal("wrote through symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep\n" {
		t.Fatalf("victim modified: %q", b)
	}
}

func TestBadHostFieldsDroppedGoodLineStillMatches(t *testing.T) {
	srv := startTestSSHServer(t)
	dir := t.TempDir()
	cfg := srv.clientConfig(filepath.Join(dir, "app"))
	user := filepath.Join(dir, "known_hosts")
	k := knownhosts.Line([]string{"x"}, srv.hostKey)
	keyPart := k[strings.Index(k, " ")+1:]
	good := knownhosts.Line([]string{knownhosts.Normalize(net.JoinHostPort(srv.host, itoa(srv.port)))}, srv.hostKey)
	var b strings.Builder
	for _, h := range []string{"|1|bad|bad", "|2|AAAA|AAAA", "!", "[unbalanced", "host,!", "|1|AAAA"} {
		b.WriteString(h + " " + keyPart + "\n")
	}
	b.WriteString(good + "\n")
	os.WriteFile(user, []byte(b.String()), 0o600)
	cfg.KnownHostsFiles = []string{user}
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func serveAgent(t *testing.T, keyring agent.Agent) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { agent.ServeAgent(keyring, c); c.Close() }()
		}
	}()
	return sock
}

func trustedConfig(t *testing.T, srv *testSSHServer) SSHConfig {
	cfg := srv.clientConfig(filepath.Join(t.TempDir(), "known_hosts"))
	fp := ssh.FingerprintSHA256(srv.hostKey)
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey, fp); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestKeyFileUsedWhenAgentIsEmpty(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	t.Setenv("SSH_AUTH_SOCK", serveAgent(t, agent.NewKeyring()))
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestAgentKeyWorksWithoutKeyFiles(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	cfg.KeyPath = ""
	t.Setenv("HOME", t.TempDir()) // no default key files
	raw, _ := os.ReadFile(srv.keyPath)
	priv, err := ssh.ParseRawPrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	kr := agent.NewKeyring()
	if err := kr.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_AUTH_SOCK", serveAgent(t, kr))
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestSlowPassphraseDoesNotConsumeHandshakeTimeout(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	srv.encryptKey("pw")
	cfg.Passphrase = func(string) (string, error) { time.Sleep(400 * time.Millisecond); return "pw", nil }
	d := NewSSH(cfg).(*sshDialer)
	d.handshakeTimeout = 200 * time.Millisecond
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestWaiterHonoursContextWhileAnotherDialRuns(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	srv.encryptKey("pw")
	release := make(chan struct{})
	started := make(chan struct{})
	cfg.Passphrase = func(string) (string, error) { close(started); <-release; return "pw", nil }
	d := NewSSH(cfg)
	defer d.Close()
	first := make(chan error, 1)
	go func() {
		c, err := d.DialAgent(context.Background())
		if c != nil {
			c.Close()
		}
		first <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	_, err := d.DialAgent(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(t0) > time.Second {
		t.Fatalf("waiter: %v after %v", err, time.Since(t0))
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func keyringWithClientKey(t *testing.T, srv *testSSHServer) agent.Agent {
	t.Helper()
	raw, _ := os.ReadFile(srv.keyPath)
	priv, err := ssh.ParseRawPrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	kr := agent.NewKeyring()
	if err := kr.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	return kr
}

func TestAgentKeyMeansNoPassphrasePrompt(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	kr := keyringWithClientKey(t, srv)
	srv.encryptKey("pw") // KeyPath now encrypted
	t.Setenv("SSH_AUTH_SOCK", serveAgent(t, kr))
	calls := 0
	cfg.Passphrase = func(string) (string, error) { calls++; return "pw", nil }
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if calls != 0 {
		t.Fatalf("passphrase prompted %d times", calls)
	}
}

func TestEncryptedKeyPromptsOnceAcrossRedial(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	srv.encryptKey("pw")
	calls := 0
	cfg.Passphrase = func(string) (string, error) { calls++; return "pw", nil }
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	srv.restart()
	c, err = d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if calls != 1 {
		t.Fatalf("passphrase prompted %d times, want 1", calls)
	}
}

func TestDeclinedPassphraseIsNotFatalAndNotRepeated(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	srv.encryptKey("pw")
	calls := 0
	cfg.Passphrase = func(string) (string, error) { calls++; return "", errors.New("declined") }
	d := NewSSH(cfg)
	defer d.Close()
	_, err := d.DialAgent(context.Background())
	if err == nil || strings.Contains(err.Error(), "declined") && !strings.Contains(err.Error(), "authenticate") {
		t.Fatalf("want auth failure, got %v", err)
	}
	d.DialAgent(context.Background())
	if calls != 1 {
		t.Fatalf("prompted %d times, want 1", calls)
	}
}

func TestUnreadableKeyPathDoesNotBlockAgent(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	cfg.KeyPath = filepath.Join(t.TempDir(), "missing")
	t.Setenv("SSH_AUTH_SOCK", serveAgent(t, keyringWithClientKey(t, srv)))
	d := NewSSH(cfg)
	defer d.Close()
	c, err := d.DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestUnreadableKeyPathWithNoSignersIsAnError(t *testing.T) {
	srv := startTestSSHServer(t)
	cfg := trustedConfig(t, srv)
	cfg.KeyPath = filepath.Join(t.TempDir(), "missing")
	d := NewSSH(cfg)
	defer d.Close()
	_, err := d.DialAgent(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("want error naming the key file, got %v", err)
	}
}

func revokedFixture(t *testing.T, srv *testSSHServer, hostField string) SSHConfig {
	cfg := trustedConfig(t, srv)
	k := knownhosts.Line([]string{"x"}, srv.hostKey)
	keyPart := k[strings.Index(k, " ")+1:]
	f := filepath.Join(t.TempDir(), "user_kh")
	os.WriteFile(f, []byte("@revoked\t"+hostField+" "+keyPart+"\n"), 0o600)
	cfg.KnownHostsFiles = []string{f}
	return cfg
}

func TestRevokedLineSurvivesMalformedHostField(t *testing.T) {
	for _, hf := range []string{"[unbalanced", "|1|bad|bad", "!", "valid.example"} {
		srv := startTestSSHServer(t)
		cfg := revokedFixture(t, srv, hf)
		d := NewSSH(cfg)
		_, err := d.DialAgent(context.Background())
		d.Close()
		var re *knownhosts.RevokedError
		if !errors.As(err, &re) {
			t.Errorf("host field %q: want revoked error, got %v", hf, err)
		}
	}
}

func TestTrustRefusesSymlinkedParentDir(t *testing.T) {
	key := newTestSigner(t).PublicKey()
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.Mkdir(real, 0o755)
	link := filepath.Join(dir, "link")
	os.Symlink(real, link)
	if err := TrustHostKey(filepath.Join(link, "kh"), "h.example", 22, key, ssh.FingerprintSHA256(key)); err == nil {
		t.Fatal("wrote through symlinked dir")
	}
	if st, _ := os.Stat(real); st.Mode().Perm() != 0o755 {
		t.Fatalf("target dir chmodded: %v", st.Mode().Perm())
	}
}
