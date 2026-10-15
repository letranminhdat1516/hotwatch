package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hotwatch/agent"
)

func TestValidateListen(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:7171", "100.64.0.1:7171", "100.101.102.103:80", "100.127.255.254:65535"} {
		if err := validateListen(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"0.0.0.0:7171", ":7171", "[::]:7171", "[::1]:7171", "167.253.157.57:7171", "10.0.0.5:7171",
		"192.168.1.2:7171", "127.0.0.2:7171", "100.63.255.255:7171", "100.128.0.0:7171", "localhost:7171",
		"example.com:7171", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:70000", "127.0.0.1:abc", "", "127.0.0.1:7171 ",
		"[::ffff:127.0.0.1]:7171", "0127.0.0.1:7171", "0x7f.0.0.1:7171",
	} {
		if err := validateListen(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestCheckAssigned(t *testing.T) {
	local := func() ([]net.IP, error) { return []net.IP{net.ParseIP("100.100.1.1"), net.ParseIP("10.0.0.2")}, nil }
	if err := checkAssigned("127.0.0.1:7171", local); err != nil {
		t.Errorf("loopback needs no interface check: %v", err)
	}
	if err := checkAssigned("100.100.1.1:7171", local); err != nil {
		t.Errorf("assigned tailscale ip: %v", err)
	}
	if err := checkAssigned("100.100.9.9:7171", local); err == nil {
		t.Error("tailscale address not on any interface must be refused")
	}
	if err := checkAssigned("0.0.0.0:7171", local); err == nil {
		t.Error("wildcard must be refused")
	}
}

func runInit(t *testing.T, tsIP func() (string, error), args ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	err := cmdInit(args, &out, &errb, tsIP)
	return out.String(), errb.String(), err
}

func noTailscale() (string, error) { return "", os.ErrNotExist }

func TestInitSSHWritesLoopbackAndPrintsTokenOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	out, _, err := runInit(t, noTailscale, "--mode", "ssh", "--config", p)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := agent.LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:7171" || cfg.ReadOnly {
		t.Fatalf("cfg = %+v", cfg)
	}
	token := strings.TrimSpace(out)
	if len(token) != 43 || strings.ContainsAny(token, " \n") {
		t.Fatalf("stdout must be only the token, got %q", out)
	}
	if agent.SHA256Hex(token) != cfg.TokenSHA256 {
		t.Fatal("stored digest does not match printed token")
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), token) {
		t.Fatal("plain token stored on disk")
	}
	if cfg.IntervalMs != 2000 {
		t.Fatalf("intervalMs = %d", cfg.IntervalMs)
	}
}

func TestInitPortAndReadOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	if _, _, err := runInit(t, noTailscale, "--mode", "ssh", "--port", "8123", "--read-only", "--config", p); err != nil {
		t.Fatal(err)
	}
	cfg, _ := agent.LoadConfig(p)
	if cfg.Listen != "127.0.0.1:8123" || !cfg.ReadOnly {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestInitTailscaleUsesTailscaleIP(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	if _, _, err := runInit(t, func() (string, error) { return "100.88.1.2\n", nil }, "--mode", "tailscale", "--config", p); err != nil {
		t.Fatal(err)
	}
	cfg, _ := agent.LoadConfig(p)
	if cfg.Listen != "100.88.1.2:7171" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
}

func TestInitTailscaleRefusesNonTailscaleAddress(t *testing.T) {
	for _, ip := range []string{"167.253.157.57", "0.0.0.0", "10.1.1.1", "garbage", "", "100.88.1.2 100.88.1.3"} {
		p := filepath.Join(t.TempDir(), "agent.json")
		_, _, err := runInit(t, func() (string, error) { return ip, nil }, "--mode", "tailscale", "--config", p)
		if err == nil {
			t.Errorf("tailscale ip %q must be refused", ip)
		}
		if _, statErr := os.Stat(p); statErr == nil {
			t.Errorf("config written for %q", ip)
		}
	}
	p := filepath.Join(t.TempDir(), "agent.json")
	if _, _, err := runInit(t, noTailscale, "--mode", "tailscale", "--config", p); err == nil {
		t.Error("tailscale mode without tailscale must fail")
	}
}

func TestInitRefusesBadFlags(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	for _, args := range [][]string{
		{"--mode", "public"}, {"--mode", "ssh", "--port", "0"}, {"--mode", "ssh", "--port", "99999"},
		{"--mode", "ssh", "--listen", "0.0.0.0:7171"}, {"--mode", "ssh", "--token", "abc"},
		{"--mode", "ssh", "extra"},
	} {
		if _, _, err := runInit(t, noTailscale, append(args, "--config", p)...); err == nil {
			t.Errorf("%v must be refused", args)
		}
	}
}

func TestInitDefaultModeIsSSHWhenTailscaleAbsent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	if _, _, err := runInit(t, noTailscale, "--config", p); err != nil {
		t.Fatalf("default mode without tailscale: %v", err)
	}
	cfg, _ := agent.LoadConfig(p)
	if cfg.Listen != "127.0.0.1:7171" {
		t.Fatalf("listen = %q", cfg.Listen)
	}
}

func TestInitRefusesExistingConfigUnlessRotate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	out1, _, _ := runInit(t, noTailscale, "--mode", "ssh", "--config", p)
	before, _ := agent.LoadConfig(p)
	if _, _, err := runInit(t, noTailscale, "--mode", "ssh", "--config", p); err == nil {
		t.Fatal("re-init must refuse without --rotate")
	}
	if after, _ := agent.LoadConfig(p); after != before {
		t.Fatal("refused init changed the config")
	}
	out2, _, err := runInit(t, noTailscale, "--mode", "ssh", "--rotate", "--config", p)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := agent.LoadConfig(p)
	if out1 == out2 || after.TokenSHA256 == before.TokenSHA256 || agent.SHA256Hex(strings.TrimSpace(out2)) != after.TokenSHA256 {
		t.Fatal("rotate must install a new token")
	}
}

type stepClock struct{ now time.Time }

func (c *stepClock) Now() time.Time { return c.now }
func (c *stepClock) After(d time.Duration) <-chan time.Time {
	c.now = c.now.Add(d)
	ch := make(chan time.Time, 1)
	ch <- c.now
	return ch
}

func TestWaitAssignedRetriesUntilAddressAppears(t *testing.T) {
	calls := 0
	lister := func() ([]net.IP, error) {
		calls++
		if calls < 4 {
			return nil, nil
		}
		return []net.IP{net.ParseIP("100.100.1.1")}, nil
	}
	clk := &stepClock{now: time.Unix(0, 0)}
	if err := waitAssigned("100.100.1.1:7171", lister, clk, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
	if calls != 4 || clk.now.Sub(time.Unix(0, 0)) != 6*time.Second {
		t.Fatalf("calls=%d waited=%v", calls, clk.now.Sub(time.Unix(0, 0)))
	}
}

func TestWaitAssignedGivesUpAfter60s(t *testing.T) {
	clk := &stepClock{now: time.Unix(0, 0)}
	err := waitAssigned("100.100.1.1:7171", func() ([]net.IP, error) { return nil, nil }, clk, func(string, ...any) {})
	if err == nil || clk.now.Sub(time.Unix(0, 0)) < 60*time.Second || clk.now.Sub(time.Unix(0, 0)) > 62*time.Second {
		t.Fatalf("err=%v waited=%v", err, clk.now.Sub(time.Unix(0, 0)))
	}
}

func TestWaitAssignedDoesNotRetryInvalidAddress(t *testing.T) {
	clk := &stepClock{now: time.Unix(0, 0)}
	if err := waitAssigned("0.0.0.0:7171", func() ([]net.IP, error) { return nil, nil }, clk, func(string, ...any) {}); err == nil || !clk.now.Equal(time.Unix(0, 0)) {
		t.Fatalf("invalid address must fail immediately (err=%v)", err)
	}
}

func TestRotateWarnsOldTokenStops(t *testing.T) {
	p := filepath.Join(t.TempDir(), "agent.json")
	runInit(t, noTailscale, "--mode", "ssh", "--config", p)
	_, errOut, err := runInit(t, noTailscale, "--mode", "ssh", "--rotate", "--config", p)
	if err != nil || !strings.Contains(errOut, "old token stops working within 5 seconds") {
		t.Fatalf("stderr = %q, err=%v", errOut, err)
	}
}
