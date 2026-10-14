//go:build darwin

package darwin

import (
	"context"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"hotwatch/model"
)

const lsofFixture = `p100
cmDNSResponder
f8
tIPv4
PUDP
n*:5353
f9
tIPv6
PUDP
n*:5353
f10
tIPv4
PUDP
n192.168.1.2:5000->17.0.0.1:443
p200
cpython3
f4
tIPv4
PTCP
n127.0.0.1:8765
f5
tIPv6
PTCP
n[::1]:8765
f6
tIPv6
PTCP
n*:9000
p300
cweird
f3
tIPv4
PTCP
nbogus
`

func TestParseLsofPorts(t *testing.T) {
	got := parseLsofPorts([]byte(lsofFixture))
	want := map[int32][]model.Port{
		100: {{Number: 5353, Proto: "udp"}},
		200: {{Number: 8765, Proto: "tcp"}, {Number: 9000, Proto: "tcp"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLsofPorts = %v, want %v", got, want)
	}
	if len(parseLsofPorts(nil)) != 0 {
		t.Fatal("empty input must give empty map")
	}
}

func TestAppBundle(t *testing.T) {
	cases := map[string]string{
		"/Applications/Safari.app/Contents/MacOS/Safari":                     "/Applications/Safari.app",
		"/Applications/Foo.app/Contents/Frameworks/Bar.app/Contents/MacOS/x": "/Applications/Foo.app/Contents/Frameworks/Bar.app",
		"/usr/bin/python3": "",
		"":                 "",
	}
	for in, want := range cases {
		if got := appBundle(in); got != want {
			t.Errorf("appBundle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapCommand(t *testing.T) {
	s := strings.Repeat("é", 3000) // 6000 bytes
	got := capCommand(s)
	if len(got) > 4096 || !utf8.ValidString(got) {
		t.Fatalf("len=%d valid=%v", len(got), utf8.ValidString(got))
	}
	if capCommand("ls -l") != "ls -l" {
		t.Fatal("short command changed")
	}
}

func TestProcessesHTTPServer(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	before := time.Now()
	cmd := exec.Command("python3", "-m", "http.server", "8765", "--bind", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	for i := 0; ; i++ {
		c, err := net.DialTimeout("tcp", "127.0.0.1:8765", 200*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if i > 50 {
			t.Fatal("server did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	col := New().(*collector)
	procs, err := col.Processes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found, launchd bool
	for _, p := range procs {
		switch p.PID {
		case int32(cmd.Process.Pid):
			found = true
			if d := p.StartTime.Sub(before); d < -2*time.Second || d > 5*time.Second {
				t.Errorf("StartTime %v not near %v", p.StartTime, before)
			}
			has := false
			for _, pt := range p.Ports {
				if pt == (model.Port{Number: 8765, Proto: "tcp"}) {
					has = true
				}
			}
			if !has {
				t.Errorf("ports = %v, want 8765/tcp", p.Ports)
			}
			if p.PPID != int32(os.Getpid()) || p.RSS == 0 || p.Threads == 0 || !strings.Contains(p.Command, "http.server") {
				t.Errorf("bad fields: %+v", p)
			}
		case 1:
			launchd = true
			if os.Geteuid() != 0 && (p.DiskReadBps != nil || p.DiskWriteBps != nil) {
				t.Errorf("launchd disk rates must be nil for non-root")
			}
			if p.StartTime.IsZero() {
				t.Errorf("launchd StartTime zero")
			}
		}
	}
	if !found || !launchd {
		t.Fatalf("found server=%v launchd=%v among %d", found, launchd, len(procs))
	}
}

func TestProcessesSelfRatesAfterSecondCall(t *testing.T) {
	col := New().(*collector)
	t0 := time.Unix(1000, 0)
	col.now = func() time.Time { return t0 }
	find := func() model.Process {
		ps, err := col.Processes(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.PID == int32(os.Getpid()) {
				return p
			}
		}
		t.Fatal("self not found")
		return model.Process{}
	}
	first := find()
	if first.DiskReadBps != nil || first.DiskWriteBps != nil {
		t.Error("first sight must have nil rates")
	}
	f, _ := os.CreateTemp(t.TempDir(), "w")
	buf := make([]byte, 1<<20)
	for i := 0; i < 8; i++ {
		f.Write(buf)
	}
	f.Sync()
	t0 = t0.Add(2 * time.Second)
	second := find()
	if second.DiskReadBps == nil || second.DiskWriteBps == nil {
		t.Fatal("second sample must have rates for own process")
	}
}

func BenchmarkProcesses(b *testing.B) {
	col := New().(*collector)
	ctx := context.Background()
	col.Processes(ctx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := col.Processes(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
