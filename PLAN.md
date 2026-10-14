# Hotwatch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A light, fast Mac monitor (mygo native UI) for the Mac itself plus user-added Linux hosts (VPS, agent-box), with processes and Docker management through a small agent reached over Tailscale or an SSH tunnel.

**Architecture:** Shared `model` types; per-OS `collect` packages; a Linux `agent` serving `/v1` JSON behind a bearer token on 127.0.0.1 or the Tailscale IP; the app's `hub` polls one `Source` per host (local in-process, remote via `transport` Direct/SSH + `agentclient`) and pushes copied state to the `ui` with `win.Update`. See ARCHITECTURE.md.

**Tech Stack:** Go 1.27, github.com/egoist/mygo (native `ui`, `ui/chart`, `Secrets`, `Shell`), github.com/shirou/gopsutil/v4 (pending spike T0.9), github.com/ebitengine/purego, golang.org/x/crypto/ssh, golang.org/x/sys, Docker Engine API v1.45 over the unix socket, Lucide icons (ISC).

**Spec:** REQUIREMENTS.md (R1–R25, answered Q1–Q7, B9) and ARCHITECTURE.md.

## Global Constraints

- Module path `hotwatch`; Go 1.27 (`go.mod`: `go 1.27`); `CGO_ENABLED=0` for every build (app and agent).
- Clean-room (R19): never open `~/Downloads/Pulse.0.1.14-decompiled/` or the Pulse binary; no Pulse name, strings, icons or layouts copied. Sources: OS docs, Docker Engine API docs, mygo docs.
- Agent never listens on a public or wildcard address; only 127.0.0.1 or the host's Tailscale IPv4 (R15).
- Process environments are never read or transmitted; container logs never written to disk (Security section).
- All external commands run with argv lists, never through a shell.
- No Telegram or any outbound notification service (B7).
- Rates and deltas that are unknown are `nil` and shown as "—", never 0.
- Heavy work (full test suite, Linux tests, agent builds, benchmarks) runs on agent-box via `/cicca-run`; only darwin-specific tests and the app run on the Mac. The Mac repo is the source of truth; never edit `~/src/hotwatch` on agent-box.
- Nothing is installed on the VPS or agent-box (agent, systemd unit, user `hotwatch`) without the owner's explicit approval at that moment.
- Commit per task, Conventional Commit subject with requirement IDs, e.g. `feat(R14): docker restart action`.

## Review Focus

1. **Laptop sleep / counter reset**: after sleep or a NIC reset, counters go backwards or jump; a reasonable person expects "—" for one sample, never negative or absurd rates. Test in T0.2 (`model.Rate`) and A1/B1 delta code.
2. **Huge process lists** (3,000+ processes on agent-box with containers): the table must stay smooth and the payload must stay under the 8 MB cap. Test in A2 (3,000-process fixture under budget) and E5 (Tester with 3,000 rows renders < 100 ms).
3. **PID reused between list and kill**: terminating must never hit a different process. Test in C3 (start-time mismatch → 409, nothing signalled).
4. **Hostile or messy container logs** (16 MB single line, ANSI escapes, binary bytes, multiplexed frames split across reads): pane stays responsive and text is readable. Test in C1 (line cap 16 KB with "…" marker, control chars stripped except tab, frame boundary split).
5. **Deleting a group that still has hosts / two hosts with the same name**: nothing disappears silently. Test in T0.5 (hosts move to the parent group; duplicate names allowed, ids unique).

---

## Phase 0 — Contracts (sequential)
| Task | Requirements | Files | Proof (test/command) |
|---|---|---|---|
| T0.1 Scaffold repo, Makefile, cicca mirror | R22 | `go.mod`, `Makefile`, `cmd/hotwatch/main.go`, `cmd/hotwatch-agent/main.go`, `.gitignore` | `make build` on Mac; `/cicca-run hotwatch make test` passes (0 tests) |
| T0.2 Shared model types + JSON round trip + `Rate` | R1–R5, R8, R12, R14 | `model/*.go`, `model/model_test.go` | `go test ./model/` |
| T0.3 `Source` / `Collector` interfaces + `source.Fake` | R16 | `source/source.go`, `source/fake.go`, `collect/collect.go`, tests | `go test ./source/ ./collect/` |
| T0.4 `hub.ConnState` machine + backoff | R11, R16 | `hub/connstate.go`, `hub/connstate_test.go` | `go test ./hub/ -run ConnState` |
| T0.5 `store` config v1, atomic write, corrupt recovery, groups, `Secrets` iface | R11, R20, R24 | `store/*.go`, tests | `go test ./store/` |
| T0.6 `docker.Engine` iface + `FakeEngine` (unix socket) | R14 | `docker/engine.go`, `docker/fake.go`, tests | `go test ./docker/ -run Fake` (agent-box) |
| T0.7 Agent HTTP contract: server skeleton, auth, read-only, limits, audit; `agentclient` | R12, R15 | `agent/server.go`, `agent/auth.go`, `agent/audit.go`, `agentclient/client.go`, `agent/contract_test.go` | `go test ./agent/ ./agentclient/` (agent-box) |
| T0.8 `transport.Dialer` iface + `Direct` | R11 | `transport/transport.go`, `transport/direct.go`, test | `go test ./transport/ -run Direct` |
| T0.9 Spike: gopsutil darwin no-cgo + process scan benchmark; record decision | R17 | `spike/gopsutil_test.go` (deleted after), `NOTES.md` | `CGO_ENABLED=0 go test -bench . ./spike/` on Mac; decision line in NOTES.md |

## Phase 1 — Parallel tracks
| Track | Task | Requirements | Files owned | Proof |
|---|---|---|---|---|
| A Linux collectors | A1 CPU, memory, load, uptime from `/proc` | R1–R3, R12 | `collect/linux/{cpu,mem,load}.go`, `collect/linux/testdata/` | `go test ./collect/linux/ -run 'CPU|Mem|Load'` (agent-box) |
| A | A2 Disks, network, processes, listening ports | R4, R5, R8, R13 | `collect/linux/{disk,net,proc,ports}.go` | `go test ./collect/linux/` incl. 3,000-process fixture benchmark |
| A | A3 NVIDIA GPU via `nvidia-smi` | R6 (Should) | `gpu/nvidia/*.go` | `go test ./gpu/nvidia/`; on agent-box compare to `nvidia-smi` |
| B Darwin collectors | B1 CPU, memory, disks, network, load, uptime | R1–R5 | `collect/darwin/{cpu,mem,disk,net,sys}.go` | `go test ./collect/darwin/` on Mac; values vs `vm_stat`, `df`, `sysctl` |
| B | B2 Processes, ports, per-process disk I/O | R4, R5, R8 | `collect/darwin/{proc,ports}.go` | `go test ./collect/darwin/ -run Proc` on Mac (`sleep 1000` found with ports) |
| B | B3 Apple GPU + power/battery/thermal/assertions | R6, R7 (Should) | `gpu/apple/*.go`, `power/*.go` | `go test ./gpu/apple/ ./power/` on Mac vs `pmset -g batt` |
| C Agent | C1 Real Docker Engine client + log demux | R14 | `docker/client.go`, `docker/logs.go` | `go test ./docker/` (FakeEngine) + `-tags dockerint` on agent-box real Docker |
| C | C2 Agent wiring: sampler lifecycle, `init --mode`, systemd unit, install script | R12, R15, R17, R22 | `agent/sampler.go`, `agent/handlers.go`, `cmd/hotwatch-agent/*`, `deploy/hotwatch-agent.service`, `deploy/install.sh` | `go test ./agent/`; `make agent` builds linux/amd64+arm64 |
| C | C3 Process signal with start-time guard | R13 | `agent/signal.go`, `agent/signal_test.go` | `go test ./agent/ -run Signal` |
| D Transport + hub | D1 SSH dialer with known_hosts states | R11, R15, B9 | `transport/ssh.go`, `transport/knownhosts.go`, tests with in-process SSH server | `go test ./transport/` |
| D | D2 `source/local`, `source/remote`, `Poller` tiering, `History` | R1, R16, R17, R25 | `source/local/*.go`, `source/remote/*.go`, `hub/poller.go`, `hub/history.go` | `go test ./source/... ./hub/` |
| D | D3 `ActionRunner` | R8, R13, R14 | `hub/action.go`, `hub/action_test.go` | `go test ./hub/ -run Action` |
| E UI | E1 First page: Overview on `source.Fake` | R1, R18 | `ui/app.go`, `ui/pages/overview.go`, `ui/app_test.go` | `go test ./ui/ -run Overview` (Tester) |
| E | E2 **Shared base layout** right after E1: `ui.Shell` (top bar R23, content, right host panel slot), theme tokens, components (card, sparkline, timechart with unit + current value, badge, banner, empty state) | R18, R23 | `ui/shell.go`, `ui/theme.go`, `ui/components/*.go` | `go test ./ui/ -run Shell` |
| E | E3 Host panel tree, add/edit host dialog (direct/SSH), move to group, host-key dialog | R11, R24, B9 | `ui/hostpanel/*.go` | `go test ./ui/hostpanel/` |
| E | E4 CPU, Memory, Disk, Network pages | R2–R5 | `ui/pages/{cpu,memory,disk,network}.go` | `go test ./ui/pages/ -run 'CPU|Memory|Disk|Network'` |
| E | E5 Processes page: table, columns, tree, inspector, terminate | R8, R13 | `ui/pages/processes*.go` | `go test ./ui/pages/ -run Processes` incl. 3,000 rows < 100 ms |
| E | E6 Docker page + logs pane | R14 | `ui/pages/docker*.go` | `go test ./ui/pages/ -run Docker` |
| E | E7 ⌘1…⌘9 and arrow navigation | R23 | `ui/shortcuts.go` | `go test ./ui/ -run Shortcut` |
| E | E8 GPU and Energy pages | R6, R7 (Should) | `ui/pages/{gpu,energy}.go` | `go test ./ui/pages/ -run 'GPU|Energy'` |
| E | E9 Settings + i18n en/vi | R20 (Should) | `ui/pages/settings.go`, `ui/i18n/*.go` | `go test ./ui/ -run 'Settings|I18n'` |
| F Mac extras | F1 `mac/apps` + Apps page | R9 (Should) | `mac/apps/*.go`, `ui/pages/apps.go` | `go test ./mac/apps/ ./ui/pages/ -run Apps` on Mac |
| F | F2 `mac/pkgs` + Packages page with task log | R10 (Should) | `mac/pkgs/*.go`, `ui/pages/packages.go` | `go test ./mac/pkgs/` (fixtures) |
| F | F3 Menu bar item | R21 (Could) | `ui/tray.go` | manual check on Mac |

Track dependencies: all tracks start after Phase 0. Inside a track tasks are sequential. E3 needs D1's `transport.HostKeyError` type (defined in T0.8, so no wait). E5/E6 use D3's `Action` (defined in T0.4/T0.3 contracts; D3 implements the runner, E5/E6 test against `hub.FakeRunner` from T0.3).

## Phase 2 — Integration + full data run

| Task | Requirements | Files | Proof |
|---|---|---|---|
| I1 Wire `cmd/hotwatch`: store → hub → window; timing logs `ui.first_data`, `ui.switch` | R11, R16, R25 | `cmd/hotwatch/main.go`, `cmd/hotwatch/wire.go` | `make run` on Mac shows This Mac live |
| I2 Install agent on agent-box (direct, Tailscale) — **ask owner first** | R12, R15 | — | app shows agent-box online, GPU card matches `nvidia-smi` |
| I3 Install agent on VPS in SSH mode (127.0.0.1) — **ask owner first** | R12, R14, R15, B9 | — | app via SSH tunnel shows VPS online; `curl 167.253.157.57:7171` fails; `jobhunter-bot` listed; restart + logs work |
| I4 Measure fast/light | R17, R25 | `scripts/measure-app.sh`, `scripts/measure-agent.sh` | numbers recorded in `/dev-verify`; pass thresholds in ARCHITECTURE.md |
| I5 Bundle `.app`/`.dmg`, `DEMO_FLOWS.md`, reset command | R22 | `mygo.json` (bundle config), `DEMO_FLOWS.md`, `scripts/reset-demo.sh` | `make dmg`; `.app` launches from Finder |

## Cut order if late

Time budget is "none", but if we need to stop early, cut from the end:
1. F3 menu bar item (R21, Could)
2. E9 Vietnamese language (keep the interval setting)
3. F2 Packages page (R10)
4. F1 Apps page (R9)
5. E8 + B3 Energy page and Apple GPU/power (R7, R6 on Mac)
6. A3 NVIDIA GPU (R6 on agent-box)
7. Process tree view and column reorder in E5 (keep sort, filter, inspector, terminate)
8. E7 keyboard shortcuts (keep clicks)

Never cut: Phase 0, A1–A2, B1–B2, C1–C3, D1–D3, E1–E6, I1–I4 (all Must).

---

# Task details

Conventions for every task: write the failing test, run it and see it fail, implement the minimum, run it and see it pass, commit with the requirement IDs. Commands marked **(agent-box)** run through `/cicca-run hotwatch <command>`; others run on the Mac.

### T0.1: Scaffold repo, Makefile, cicca mirror

**Files:** Create `go.mod`, `Makefile`, `cmd/hotwatch/main.go`, `cmd/hotwatch-agent/main.go`; modify `.gitignore`.

**Interfaces:** Produces make targets `build`, `test`, `test-darwin`, `agent`, `run`, `dmg` used by every later task.

- [ ] **Step 1: Create the module and entry points**

```bash
cd ~/src/hotwatch
go mod init hotwatch && go mod edit -go=1.27
go get github.com/egoist/mygo@latest golang.org/x/sys@latest golang.org/x/crypto@latest
```

```go
// cmd/hotwatch/main.go
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Hotwatch", Content: ui.View(func(c *ui.Context) {
			ui.Text(c, "Hotwatch")
		})})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

```go
// cmd/hotwatch-agent/main.go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "hotwatch-agent: not implemented yet")
	os.Exit(2)
}
```

- [ ] **Step 2: Makefile**

```make
export CGO_ENABLED=0
GO ?= go
.PHONY: build test test-darwin agent run dmg
build:
	$(GO) build ./...
test:            # Linux + portable packages (run on agent-box)
	$(GO) test ./... 
test-darwin:     # macOS-only packages (run on the Mac)
	$(GO) test ./collect/darwin/... ./gpu/apple/... ./power/... ./mac/... ./ui/...
agent:
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -o dist/hotwatch-agent-linux-amd64 ./cmd/hotwatch-agent
	GOOS=linux GOARCH=arm64 $(GO) build -trimpath -o dist/hotwatch-agent-linux-arm64 ./cmd/hotwatch-agent
run:
	$(GO) run ./cmd/hotwatch
dmg:
	$(GO) tool mygo build
```

Add `dist/` to `.gitignore`.

- [ ] **Step 3: Register the mirror and prove both sides build**

```bash
echo "$HOME/src/hotwatch" >> ~/.config/cicca/repos
~/.claude/skills/cicca/scripts/sync.sh ~/src/hotwatch
make build
```
Then **(agent-box)** `make test` → `ok` / `no test files`.

- [ ] **Step 4: Commit** `chore(R22): scaffold module, entry points, Makefile`

### T0.2: Shared model types + JSON round trip + `Rate`

**Files:** Create `model/host.go`, `model/snapshot.go`, `model/process.go`, `model/container.go`, `model/errors.go`, `model/rate.go`, `model/model_test.go`.

**Interfaces:** Produces every type in ARCHITECTURE.md "Data model", plus:
- `func Rate(prev, cur uint64, dt time.Duration) *float64` — nil when `dt <= 0`, `cur < prev` (counter reset) or `dt > 5*time.Minute` (sleep).
- `type Signal string` with `SigTerm = "TERM"`, `SigKill = "KILL"`; `type ContainerAction string` with `Start`, `Stop`, `Restart`; `type LogLine struct{ Time time.Time; Stream, Text string }`.
- `const ProtocolVersion = "1.0"`; `func Compatible(v string) bool` (same major).
- `Snapshot.Partial []string` (`json:"partial,omitempty"`): names of sections (`cpu`, `memory`, `disks`, `net`, `gpu`, `battery`) whose values were carried over or unreadable in this sample.

- [ ] **Step 1: Failing tests**

```go
package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestRate(t *testing.T) {
	if r := Rate(100, 300, 2*time.Second); r == nil || *r != 100 {
		t.Fatalf("Rate = %v, want 100", r)
	}
	for name, r := range map[string]*float64{
		"reset":   Rate(500, 100, time.Second),
		"zero dt": Rate(1, 2, 0),
		"sleep":   Rate(1, 2, 10*time.Minute),
	} {
		if r != nil {
			t.Errorf("%s: Rate = %v, want nil", name, *r)
		}
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	rx := 1.5
	in := Snapshot{Time: time.Unix(1, 0).UTC(), CPU: CPU{Total: 12.5, PerCore: []float64{10, 15}, Load: [3]float64{1, 2, 3}},
		Net: []Interface{{Name: "eth0", RxBps: &rx}}, Battery: nil}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Snapshot
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch:\n in %+v\nout %+v", in, out)
	}
}

func TestCompatible(t *testing.T) {
	if !Compatible("1.3") || Compatible("2.0") || Compatible("") {
		t.Fatal("Compatible wrong")
	}
}
```

- [ ] **Step 2:** `go test ./model/` → FAIL (undefined: Rate, Snapshot, Compatible).
- [ ] **Step 3: Implement** the structs exactly as in ARCHITECTURE.md "Data model" with JSON tags in lowerCamelCase (`json:"perCore"`), and:

```go
func Rate(prev, cur uint64, dt time.Duration) *float64 {
	if dt <= 0 || dt > 5*time.Minute || cur < prev {
		return nil
	}
	r := float64(cur-prev) / dt.Seconds()
	return &r
}

func Compatible(v string) bool {
	major, _, _ := strings.Cut(v, ".")
	want, _, _ := strings.Cut(ProtocolVersion, ".")
	return major != "" && major == want
}
```

- [ ] **Step 4:** `go test ./model/` → PASS.
- [ ] **Step 5: Commit** `feat(R1-R5,R8,R12,R14): shared model types and Rate`

### T0.3: `Source` / `Collector` interfaces + fakes

**Files:** Create `source/source.go`, `source/fake.go`, `source/fake_test.go`, `collect/collect.go`, `hub/runner.go` (interface + `FakeRunner` only).

**Interfaces:** Produces `source.Source` and `collect.Collector` exactly as in ARCHITECTURE.md "Interfaces / contracts", plus:

```go
// source/fake.go
type Fake struct {
	Mu         sync.Mutex
	InfoV      model.HostInfo
	Snaps      []model.Snapshot // returned in order, last one repeats
	Procs      []model.Process
	Ctrs       []model.Container
	Err        error            // returned by every call while non-nil
	Delay      time.Duration
	Signals    []string         // "pid:TERM" records
	Actions    []string         // "id:restart" records
	Logs       []model.LogLine
}

// hub/runner.go
type Runner interface {
	Start(a Action) ActionID
	Get(id ActionID) Action
}
type ActionKind string // "signal", "container", "package", "uninstall"
type ActionState string // "confirming","running","succeeded","failed","cancelled"
type Action struct {
	ID ActionID; Kind ActionKind; HostID, Target, Verb string
	State ActionState; Err *model.APIError; Log []string
}
type ActionID int64
type FakeRunner struct{ Started []Action }
```

- [ ] **Step 1: Failing test**

```go
func TestFakeSequenceAndError(t *testing.T) {
	f := &Fake{Snaps: []model.Snapshot{{CPU: model.CPU{Total: 1}}, {CPU: model.CPU{Total: 2}}}}
	ctx := context.Background()
	for _, want := range []float64{1, 2, 2} {
		s, err := f.Snapshot(ctx)
		if err != nil || s.CPU.Total != want {
			t.Fatalf("got %v %v, want %v", s.CPU.Total, err, want)
		}
	}
	f.Err = errors.New("down")
	if _, err := f.Snapshot(ctx); err == nil {
		t.Fatal("want error")
	}
	var _ Source = f
}
```

- [ ] **Step 2:** `go test ./source/` → FAIL. **Step 3:** implement `Fake` methods (honour `Delay` with `ctx`, record `Signals`/`Actions`, `ContainerLogs` sends `Logs` then closes). **Step 4:** PASS. **Step 5: Commit** `feat(R16): Source/Collector contracts and fakes`

### T0.4: `hub.ConnState` machine + backoff

**Files:** Create `hub/connstate.go`, `hub/connstate_test.go`.

**Interfaces:** Produces

```go
type ConnState string // Connecting, Online, Stale, Offline, Unauthorized, Incompatible, HostKeyUnknown, HostKeyMismatch
type Event int        // EvOK, EvFail, EvUnauthorized, EvIncompatible, EvHostKeyUnknown, EvHostKeyMismatch, EvReset
type Conn struct{ State ConnState; Failures int; LastOK time.Time; NextTry time.Time }
func (c Conn) Next(ev Event, now time.Time) Conn
func Backoff(failures int) time.Duration // 2s,4s,8s,16s,30s,60s,60s…
```

- [ ] **Step 1: Failing table test** covering every row of ARCHITECTURE.md "Host connection":

```go
func TestConnTransitions(t *testing.T) {
	now := time.Unix(1000, 0)
	c := Conn{State: Connecting}
	steps := []struct{ ev Event; want ConnState }{
		{EvOK, Online}, {EvFail, Stale}, {EvFail, Stale}, {EvFail, Offline},
		{EvFail, Offline}, {EvOK, Online}, {EvUnauthorized, Unauthorized},
		{EvOK, Unauthorized}, {EvReset, Connecting}, {EvHostKeyUnknown, HostKeyUnknown},
		{EvReset, Connecting}, {EvHostKeyMismatch, HostKeyMismatch}, {EvFail, HostKeyMismatch},
	}
	for i, s := range steps {
		c = c.Next(s.ev, now)
		if c.State != s.want {
			t.Fatalf("step %d: %v → %v, want %v", i, s.ev, c.State, s.want)
		}
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{2, 4, 8, 16, 30, 60, 60}
	for i, w := range want {
		if got := Backoff(i + 1); got != w*time.Second {
			t.Errorf("Backoff(%d) = %v, want %v", i+1, got, w*time.Second)
		}
	}
}
```

- [ ] **Step 2:** FAIL. **Step 3:** implement `Next` as a switch on (state, event); terminal states (`Unauthorized`, `Incompatible`, `HostKeyMismatch`) only leave on `EvReset`; `Offline` sets `NextTry = now + Backoff(Failures-2)`. **Step 4:** PASS. **Step 5: Commit** `feat(R11,R16): connection state machine`

### T0.5: `store` config v1

**Files:** Create `store/config.go`, `store/groups.go`, `store/secrets.go`, `store/store_test.go`.

**Interfaces:** Produces

```go
type Conn struct{ Type string; Address string; AgentPort int; SSHHost string; SSHPort int; SSHUser, KeyPath string } // Type "direct"|"ssh"
type Host struct{ ID, Name, Kind, Group string; Conn *Conn }          // Kind "local"|"agent"
type Group struct{ ID, Name, Parent string }
type Settings struct{ IntervalMs int; Language string; Columns map[string][]string; LaunchAtLogin bool }
type UIState struct{ SelectedHost, Page string; OpenGroups []string }
type Config struct{ Version int; Groups []Group; Hosts []Host; Settings Settings; UI UIState }
func Load(path string) (Config, Warning, error) // Warning non-empty when a corrupt file was set aside
func Save(path string, c Config) error            // temp file + fsync + rename
func Default() Config                             // This Mac in group "Local", interval 2000, language "en"
func (c *Config) RemoveGroup(id string)            // hosts and child groups move to the parent
func (c *Config) MoveHost(hostID, groupID string) error
type Secrets interface{ Get(key string) (string, error); Set(key, value string) error; Delete(key string) error }
func TokenKey(hostID string) string               // "hotwatch/host/<id>"
```

- [ ] **Step 1: Failing tests**

```go
func TestCorruptConfigIsSetAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte("{not json"), 0o600)
	c, warn, err := Load(p)
	if err != nil || warn == "" || c.Hosts[0].ID != "local" {
		t.Fatalf("Load = %+v %q %v", c, warn, err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "config.json.bad-*"))
	if len(matches) != 1 {
		t.Fatalf("bad file not kept: %v", matches)
	}
}

func TestRemoveGroupMovesHostsToParent(t *testing.T) {
	c := Default()
	c.Groups = append(c.Groups, Group{ID: "g-servers", Name: "Servers"}, Group{ID: "g-eu", Name: "EU", Parent: "g-servers"})
	c.Hosts = append(c.Hosts, Host{ID: "h1", Name: "VPS", Kind: "agent", Group: "g-eu"}, Host{ID: "h2", Name: "VPS", Kind: "agent", Group: "g-eu"})
	c.RemoveGroup("g-eu")
	for _, h := range c.Hosts[1:] {
		if h.Group != "g-servers" {
			t.Fatalf("host %s in %q, want g-servers", h.ID, h.Group)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Default()
	c.Settings.IntervalMs = 5000
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, warn, err := Load(p)
	if err != nil || warn != "" || got.Settings.IntervalMs != 5000 {
		t.Fatalf("got %+v %q %v", got.Settings, warn, err)
	}
}
```

- [ ] **Step 2:** FAIL. **Step 3:** implement; missing file → `Default()` without warning; `Version != 1` → treated as corrupt. **Step 4:** PASS. **Step 5: Commit** `feat(R11,R20,R24): config store with groups`

### T0.6: `docker.Engine` interface + `FakeEngine`

**Files:** Create `docker/engine.go`, `docker/fake.go`, `docker/fake_test.go`.

**Interfaces:** Produces

```go
type Engine interface {
	List(ctx context.Context) ([]model.Container, error)          // includes stats
	Do(ctx context.Context, id string, act model.ContainerAction) error
	Logs(ctx context.Context, id string, tail int, follow bool) (<-chan model.LogLine, error)
}
// FakeEngine serves the Engine API subset on a unix socket in t.TempDir():
// GET /v1.45/containers/json?all=1, GET /v1.45/containers/{id}/stats?stream=0,
// POST /v1.45/containers/{id}/{start,stop,restart}, GET /v1.45/containers/{id}/logs (multiplexed frames)
func NewFakeEngine(t testing.TB) (socketPath string, f *FakeState)
type FakeState struct{ Mu sync.Mutex; Containers map[string]*FakeContainer; LogFrames [][]byte }
```

- [ ] **Step 1: Failing test:** start `NewFakeEngine`, `http.Get` over the socket to `/v1.45/containers/json?all=1`, expect the seeded `jobhunter-bot` JSON. **Step 2:** FAIL. **Step 3:** implement with `httptest.NewUnstartedServer` + `net.Listen("unix", path)`. **Step 4:** PASS **(agent-box)**. **Step 5: Commit** `test(R14): docker engine contract and fake`

### T0.7: Agent HTTP contract + `agentclient`

**Files:** Create `agent/server.go`, `agent/auth.go`, `agent/audit.go`, `agent/contract_test.go`, `agentclient/client.go`, `agentclient/client_test.go`.

**Interfaces:**
- Consumes `collect.Collector`, `docker.Engine`, `model.*`.
- Produces `agent.New(cfg Config, col collect.Collector, eng docker.Engine, sig Signaller, audit io.Writer) http.Handler`, `agent.Config{TokenSHA256 string; ReadOnly bool; Interval time.Duration}`, `type Signaller interface{ Signal(pid int32, start time.Time, s model.Signal) error }`, and `agentclient.New(dial func(ctx context.Context) (net.Conn, error), token string) *Client` implementing `source.Source`.
- Limits: request body 4 KB, response caps on the client side (snapshot 1 MB, processes 8 MB, containers 2 MB), per-request timeout 3 s (logs exempt).

- [ ] **Step 1: Failing contract tests** (agent and client together over `httptest`):

```go
func TestAuthAndReadOnly(t *testing.T) {
	tok := "s3cret-token"
	h := New(Config{TokenSHA256: SHA256Hex(tok), ReadOnly: true}, &collect.Fake{}, &docker.FakeEngineAPI{}, &FakeSignaller{}, io.Discard)
	srv := httptest.NewServer(h)
	defer srv.Close()

	res := do(t, srv, "GET", "/v1/info", "")
	if res.StatusCode != 401 {
		t.Fatalf("no token: %d", res.StatusCode)
	}
	res = do(t, srv, "GET", "/v1/info", "wrong")
	if res.StatusCode != 401 {
		t.Fatalf("wrong token: %d", res.StatusCode)
	}
	res = do(t, srv, "POST", "/v1/containers/jobhunter-bot/restart", tok)
	if res.StatusCode != 403 || apiCode(t, res) != "forbidden_read_only" {
		t.Fatalf("read-only restart: %d", res.StatusCode)
	}
}

func TestClientRejectsOversizedProcesses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[")); w.Write(bytes.Repeat([]byte(`{"pid":1},`), 1_000_000)); w.Write([]byte(`{"pid":2}]`))
	}))
	defer srv.Close()
	c := New(dialTo(srv), "t")
	if _, err := c.Processes(context.Background()); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want size error, got %v", err)
	}
}
```

Also: lockout after 5 bad tokens per minute (6th attempt with the right token → 429), audit line written for every POST (JSON with `time`, `remote`, `route`, `target`, `result`, no token).

- [ ] **Step 2:** FAIL **(agent-box)**. **Step 3:** implement middleware chain `limitBody → auth (subtle.ConstantTimeCompare on sha256) → readOnly (POST) → handler → audit`. **Step 4:** PASS. **Step 5: Commit** `feat(R12,R15): agent protocol contract and client`

### T0.8: `transport.Dialer` + `Direct`

**Files:** Create `transport/transport.go`, `transport/direct.go`, `transport/direct_test.go`.

**Interfaces:** Produces

```go
type Dialer interface {
	DialAgent(ctx context.Context) (net.Conn, error) // connection to the agent's HTTP port
	Close() error
}
type HostKeyError struct {
	Host        string
	Fingerprint string // "SHA256:…"
	Mismatch    bool   // true = key changed (hard stop); false = unknown (ask user)
}
func (e *HostKeyError) Error() string
func NewDirect(address string, port int) Dialer
```

- [ ] **Step 1:** failing test: `NewDirect` to a `net.Listen("tcp","127.0.0.1:0")` listener accepts; dialing a closed port returns an error within 3 s. **Step 2–4:** implement with `net.Dialer{Timeout: 3 * time.Second}`. **Step 5: Commit** `feat(R11): transport dialer contract and direct dialer`

### T0.9: Spike — gopsutil on darwin without cgo

**Files:** Create `spike/gopsutil_test.go` (deleted at the end of the task); modify `NOTES.md`.

- [ ] **Step 1:** write

```go
//go:build darwin
package spike

import (
	"testing"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

func BenchmarkProcessScan(b *testing.B) {
	for b.Loop() {
		ps, err := process.Processes()
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range ps {
			p.Name(); p.CPUPercent(); p.MemoryInfo()
		}
	}
}

func TestBasicsNoCgo(t *testing.T) {
	if _, err := cpu.Percent(0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.VirtualMemory(); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2:** `CGO_ENABLED=0 go test -bench . -benchtime 5x ./spike/` on the Mac.
- [ ] **Step 3:** record in NOTES.md: build OK yes/no, ns/op per full scan, process count. Decision rule: build OK and scan ≤ 50 ms → B1/B2 use gopsutil for those parts; otherwise B2 uses own `libproc` calls via purego (`proc_listallpids`, `proc_pidinfo` with `PROC_PIDTASKALLINFO`, `proc_pid_rusage`).
- [ ] **Step 4:** delete `spike/`, `go mod tidy`. **Step 5: Commit** `docs(R17): record collector spike decision`

---

## Track A — Linux collectors (agent-box)

### A1: CPU, memory, load, uptime

**Files:** Create `collect/linux/collector.go`, `collect/linux/cpu.go`, `collect/linux/mem.go`, `collect/linux/load.go`, `collect/linux/linux_test.go`, fixtures `collect/linux/testdata/t0/proc/{stat,meminfo,loadavg,uptime}` and `t1/proc/stat` (2 s later).

**Interfaces:** Produces `linux.New(root string) collect.Collector` (`root` = "/" in production, fixture dir in tests); per-core and total CPU from `/proc/stat` deltas; memory from `/proc/meminfo` (`Used = MemTotal - MemAvailable`, `Cached`, `Buffers`, swap); load from `/proc/loadavg`; uptime from `/proc/uptime`.

- [ ] **Step 1: Failing test**

```go
func TestCPUFromFixtures(t *testing.T) {
	c := newAt("testdata/t0", time.Unix(0, 0))
	if _, err := c.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.root, c.now = "testdata/t1", func() time.Time { return time.Unix(2, 0) }
	s, err := c.Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.CPU.PerCore) != 4 || math.Abs(s.CPU.Total-25.0) > 0.01 {
		t.Fatalf("cpu = %+v", s.CPU)
	}
	if s.Memory.Total != 8_083_000*1024 || s.Memory.Used != (8_083_000-3_120_000)*1024 {
		t.Fatalf("mem = %+v", s.Memory)
	}
}
```

Fixtures are hand-written: 4 cores, t0→t1 each core adds 100 jiffies of which 25 non-idle. Add a third fixture `t2` where the counters go down (VM migration): `Sample` keeps the previous CPU values and adds `"cpu"` to `Snapshot.Partial`, so the UI marks the CPU card as stale for one sample instead of showing a wrong value.

- [ ] **Step 2–4:** FAIL → implement parsers with `bufio.Scanner`, no regexps → PASS **(agent-box)**.
- [ ] **Step 5: Commit** `feat(R1-R3,R12): linux cpu, memory, load, uptime`

### A2: Disks, network, processes, listening ports

**Files:** Create `collect/linux/disk.go`, `collect/linux/net.go`, `collect/linux/proc.go`, `collect/linux/ports.go`, tests, fixtures (`proc/mounts`, `proc/diskstats`, `proc/net/dev`, `proc/net/tcp`, `proc/net/tcp6`, `proc/<pid>/{stat,status,cmdline,io,fd/}` for 3 pids, generator for 3,000 pids in `TestMain`).

Rules: real filesystems only (exclude `tmpfs`, `overlay`, `squashfs`, `proc`, `sysfs`, `cgroup*`, `devtmpfs`, `nsfs`); capacity via `unix.Statfs` (skipped in fixture mode, values from a fixture map); rates via `model.Rate`; processes from `/proc/<pid>/stat` (start time = boot time + starttime/CLK_TCK), user from `status` Uid mapped through `/etc/passwd` in `root`; command from `cmdline` (NUL → space, capped 4 KB); never read `environ`; ports: map socket inodes from `fd/*` links `socket:[inode]` to LISTEN rows (`st == 0A`) in `tcp`/`tcp6`, UDP from `udp`/`udp6` with `st == 07`.

- [ ] **Step 1: Failing tests:** volumes list equals `["/", "/data"]` for the fixture; `eth0` rx rate 1000 B/s; pid 4242 `python3` has port 8765/tcp; benchmark `BenchmarkProcesses3000` must report < 50 ms/op and the JSON of the result < 8 MB (assert in test).
- [ ] **Step 2–4:** FAIL → implement → PASS **(agent-box)**.
- [ ] **Step 5: Commit** `feat(R4,R5,R8,R13): linux disks, network, processes, ports`

### A3: NVIDIA GPU (Should)

**Files:** Create `gpu/nvidia/nvidia.go`, `gpu/nvidia/nvidia_test.go`.

**Interfaces:** `nvidia.Read(ctx context.Context, run func(ctx context.Context, name string, args ...string) ([]byte, error)) ([]model.GPU, error)`; production `run` = `exec.CommandContext` with a 2 s timeout; command `nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu --format=csv,noheader,nounits`; `exec.ErrNotFound` → `nil, nil` (no GPU).

- [ ] **Step 1:** failing test with canned output `NVIDIA GeForce RTX 3080, 34, 1595, 10240, 52` → one GPU, util 34, mem 1595 MiB/10240 MiB in bytes, temp 52; `[N/A]` fields → nil. **Step 2–4.** **Step 5:** on agent-box compare one reading with `nvidia-smi` (within 5 %). Commit `feat(R6): nvidia gpu reader`

## Track B — Darwin collectors (Mac)

### B1: CPU, memory, disks, network, load, uptime

**Files:** Create `collect/darwin/collector.go`, `collect/darwin/{cpu,mem,disk,net,sys}.go`, `collect/darwin/darwin_test.go` (`//go:build darwin`).

Implementation per T0.9 decision. Memory from `host_statistics64(HOST_VM_INFO64)` via purego (`libSystem`) or gopsutil: `App = internal - purgeable`, `Wired`, `Compressed = compressor_page_count * pagesize`; pressure from `sysctl kern.memorystatus_vm_pressure_level` (1 normal, 2 warn, 4 critical). Disks from `getfsstat` (skip `MNT_DONTBROWSE`, `devfs`, `autofs`, snapshots under `/System/Volumes/` except `Data`), I/O from IOKit `IOBlockStorageDriver` statistics. Network from `getifaddrs` + `sysctl NET_RT_IFLIST2`.

- [ ] **Step 1: Failing tests** compare against the OS on this Mac:

```go
func TestCoresMatchSysctl(t *testing.T) {
	c := New()
	c.Sample(context.Background())
	time.Sleep(500 * time.Millisecond)
	s, err := c.Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n, _ := unix.SysctlUint32("hw.ncpu")
	if len(s.CPU.PerCore) != int(n) {
		t.Fatalf("cores %d, sysctl %d", len(s.CPU.PerCore), n)
	}
}

func TestMemoryNearVMStat(t *testing.T) {
	s, _ := New().Sample(context.Background())
	out, _ := exec.Command("vm_stat").Output()
	free := vmStatPages(out, "Pages free") * pageSize()
	if diffPct(s.Memory.Total-s.Memory.Used, free+inactive(out)) > 5 { // same definition as Activity Monitor's "available"
		t.Fatalf("memory differs from vm_stat by > 5%%")
	}
}
```

Helpers `vmStatPages`, `inactive`, `pageSize` and `diffPct` live in `darwin_test.go` (parse `vm_stat` lines `Pages free: 12345.`; page size from `hw.pagesize`). Plus: `/` listed in `Disks`; first sample has nil rates, second has non-nil.
- [ ] **Step 2–4** on the Mac. **Step 5: Commit** `feat(R1-R5): darwin system collectors`

### B2: Processes, ports, per-process disk I/O

**Files:** Create `collect/darwin/proc.go`, `collect/darwin/ports.go`, `collect/darwin/proc_test.go`.

Process list via T0.9 decision; start time from `kinfo_proc.p_starttime` (sysctl `KERN_PROC_ALL`) or `proc_pidinfo(PROC_PIDTBSDINFO)`; ports via `proc_pidinfo(PROC_PIDLISTFDS)` + `proc_pidfdinfo(PROC_PIDFDSOCKETINFO)` for TCP LISTEN and UDP; disk I/O via `proc_pid_rusage(RUSAGE_INFO_V4)` deltas; processes the user cannot inspect keep `nil` fields; `AppBundle` = nearest enclosing `*.app` of the executable path.

- [ ] **Step 1: Failing test:** start `exec.Command("python3", "-m", "http.server", "8765", "--bind", "127.0.0.1")`, wait for the port, `Processes()` contains its PID with `Port{8765, "tcp"}` and the right `StartTime` (±1 s of `cmd.Process` start); a `launchd` (pid 1) row exists with `DiskReadBps == nil` when not root.
- [ ] **Step 2–4.** **Step 5: Commit** `feat(R4,R5,R8): darwin processes and ports`

### B3: Apple GPU and power (Should)

**Files:** Create `gpu/apple/apple.go`, `power/power.go`, `power/assertions.go`, tests (`//go:build darwin`).

GPU: IOKit `IOAccelerator` `PerformanceStatistics` dictionary keys `Device Utilization %`, `In use system memory`. Battery: `IOPSCopyPowerSourcesInfo` (charge, charging, time to empty/full); power draw from `AppleSmartBattery` `InstantAmperage × Voltage`; thermal state from `NSProcessInfo.thermalState` via purego objc; temperatures only from sensors IOKit exposes without root (`AppleSmartBattery Temperature`); sleep assertions from `IOPMCopyAssertionsByProcess`.

- [ ] **Step 1: Failing test:** battery present on this MacBook; `Charge` within 1 % of `pmset -g batt`; `Assertions` returns ≥ 0 entries without error; GPU util is not nil. **Step 2–4.** **Step 5: Commit** `feat(R6,R7): apple gpu and power`

## Track C — Agent (agent-box)

### C1: Docker Engine client + log demux

**Files:** Create `docker/client.go`, `docker/logs.go`, `docker/client_test.go`, `docker/logs_test.go`, `docker/integration_test.go` (`//go:build dockerint`).

**Interfaces:** `docker.NewClient(socket string) Engine` implementing T0.6 via `http.Client{Transport: &http.Transport{DialContext: unix}}`; API version pinned `v1.45`; stats CPU % = `(cpu_delta / system_delta) × online_cpus × 100` from one `stream=0` call; compose project from label `com.docker.compose.project`; id validation `^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$` before any request.

- [ ] **Step 1: Failing tests:**

```go
func TestLogDemuxSplitFrames(t *testing.T) {
	// two frames (stdout "hello\n", stderr "\x1b[31mred\x1b[0m\n") delivered in 3-byte chunks
	r := chunked(frames(1, "hello\n", 2, "\x1b[31mred\x1b[0m\n"), 3)
	lines := collect(t, demux(r))
	want := []model.LogLine{{Stream: "stdout", Text: "hello"}, {Stream: "stderr", Text: "red"}}
	if !sameText(lines, want) {
		t.Fatalf("got %+v", lines)
	}
}

func TestLogLineCap(t *testing.T) {
	big := strings.Repeat("a", 16<<20) + "\n"
	lines := collect(t, demux(bytes.NewReader(frames(1, big))))
	if len(lines) != 1 || len(lines[0].Text) != 16<<10+len("…") {
		t.Fatalf("len = %d", len(lines[0].Text))
	}
}
```

Plus FakeEngine-backed tests for `List` (stats parsed), `Do(restart)` hits `/restart`, invalid id `../x` rejected without a request.
- [ ] **Step 2–4 (agent-box).** **Step 5:** integration **(agent-box)**: `go test -tags dockerint ./docker/` starts `docker run -d --name hw-test busybox sh -c 'while true; do echo tick; sleep 1; done'`, lists it, restarts it, reads ≥ 1 `tick` line, removes it. Commit `feat(R14): docker engine client and log demux`

### C2: Agent wiring, sampler lifecycle, install

**Files:** Create `agent/sampler.go`, `agent/handlers.go`, `agent/config.go`, `cmd/hotwatch-agent/main.go` (replace stub), `cmd/hotwatch-agent/init.go`, `deploy/hotwatch-agent.service`, `deploy/install.sh`, tests.

Behaviour: subcommands `init --mode tailscale|ssh [--port 7171] [--read-only]` (writes `/etc/hotwatch/agent.json` 0600, prints token once; refuses any listen address other than 127.0.0.1 or the `tailscale ip -4` result), `run` (default), `version`. Sampler starts on the first authenticated request and stops 30 s after the last one. `GET /v1/info` reports `Caps` (Docker = socket reachable; GPU = `nvidia-smi` found; ReadOnly).

systemd unit:

```ini
[Unit]
Description=Hotwatch agent
After=network-online.target docker.service tailscaled.service
[Service]
User=hotwatch
Group=hotwatch
SupplementaryGroups=docker
ExecStart=/usr/local/bin/hotwatch-agent run --config /etc/hotwatch/agent.json
Restart=always
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=yes
AmbientCapabilities=CAP_KILL
CapabilityBoundingSet=CAP_KILL
ReadWritePaths=/var/log/hotwatch
[Install]
WantedBy=multi-user.target
```

`deploy/install.sh` (prints every step and asks `[y/N]` before acting; never installs other software): create user `hotwatch` if missing, copy binary to `/usr/local/bin`, `init`, install unit, `systemctl enable --now`.

- [ ] **Step 1: Failing tests:** `init` with `--mode ssh` writes `listen: 127.0.0.1:7171`; `validateListen("0.0.0.0:7171")` and `validateListen("167.253.157.57:7171")` return errors; sampler: with a fake clock, no request for 31 s → sampler stopped (`sampler.Running() == false`); a request restarts it.
- [ ] **Step 2–4 (agent-box).** **Step 5:** `make agent` produces both binaries; `systemd-analyze verify deploy/hotwatch-agent.service` passes on agent-box. Commit `feat(R12,R15,R17,R22): agent wiring, sampler lifecycle, install files`

### C3: Process signal with start-time guard

**Files:** Create `agent/signal.go`, `agent/signal_test.go`.

**Interfaces:** `agent.NewSignaller(col collect.Collector, kill func(pid int, sig syscall.Signal) error) Signaller`.

- [ ] **Step 1: Failing tests:**

```go
func TestSignalRefusesReusedPID(t *testing.T) {
	col := &collect.Fake{Procs: []model.Process{{PID: 4242, StartTime: time.Unix(200, 0)}}}
	var killed []int
	s := NewSignaller(col, func(pid int, _ syscall.Signal) error { killed = append(killed, pid); return nil })
	err := s.Signal(4242, time.Unix(100, 0), model.SigTerm)
	var apiErr *model.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "conflict" || len(killed) != 0 {
		t.Fatalf("err=%v killed=%v", err, killed)
	}
}

func TestSignalRefusesPID1AndSelf(t *testing.T) {
	s := NewSignaller(&collect.Fake{}, func(int, syscall.Signal) error { t.Fatal("must not kill"); return nil })
	for _, pid := range []int32{0, 1, int32(os.Getpid())} {
		if err := s.Signal(pid, time.Now(), model.SigKill); err == nil {
			t.Fatalf("pid %d allowed", pid)
		}
	}
}
```

Also: `EPERM` maps to `APIError{Code: "forbidden", Message: "not permitted (process owned by another user)"}`.
- [ ] **Step 2–4 (agent-box).** **Step 5: Commit** `feat(R13): process signal with start-time guard`

## Track D — Transport + hub

### D1: SSH dialer with known_hosts states

**Files:** Create `transport/ssh.go`, `transport/knownhosts.go`, `transport/ssh_test.go`.

**Interfaces:** `transport.NewSSH(cfg SSHConfig) Dialer` with `SSHConfig{Host string; Port int; User, KeyPath string; AgentPort int; KnownHostsFiles []string; AppKnownHosts string; Passphrase func(keyPath string) (string, error)}`; `transport.TrustHostKey(appKnownHosts, host string, port int, key ssh.PublicKey) error`.

Rules: auth methods in order: ssh-agent (`SSH_AUTH_SOCK`), then `KeyPath` (or `~/.ssh/id_ed25519`, `id_rsa`); host key callback built from `knownhosts.New(files...)`: unknown → `*HostKeyError{Mismatch:false}`, changed → `*HostKeyError{Mismatch:true}`; only `client.Dial("tcp", "127.0.0.1:<AgentPort>")`; no sessions or commands; one `*ssh.Client` per host reused, re-dialled after an error; keepalive `keepalive@openssh.com` every 15 s.

- [ ] **Step 1: Failing tests** with an in-process SSH server (`ssh.ServerConfig` + generated ed25519 host key) that forwards `direct-tcpip` to a local echo listener:

```go
func TestUnknownThenTrustedHostKey(t *testing.T) {
	srv := startTestSSHServer(t)          // returns addr, host key, accepted client key
	dir := t.TempDir()
	cfg := srv.clientConfig(filepath.Join(dir, "known_hosts"))
	_, err := NewSSH(cfg).DialAgent(context.Background())
	var hk *HostKeyError
	if !errors.As(err, &hk) || hk.Mismatch {
		t.Fatalf("want unknown host key, got %v", err)
	}
	if err := TrustHostKey(cfg.AppKnownHosts, cfg.Host, cfg.Port, srv.hostKey); err != nil {
		t.Fatal(err)
	}
	conn, err := NewSSH(cfg).DialAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	echo(t, conn, "ping")
}

func TestChangedHostKeyIsMismatch(t *testing.T) { /* trust key A, server presents key B → Mismatch:true */ }
func TestOnlyLoopbackForwarding(t *testing.T) { /* server records requested target; must equal 127.0.0.1:<AgentPort>; no "session" channel opened */ }
```

- [ ] **Step 2–4.** **Step 5: Commit** `feat(R11,R15): ssh tunnel transport with host key checks`

### D2: Local/remote sources, poller tiering, history

**Files:** Create `source/local/local.go`, `source/remote/remote.go`, `hub/hub.go`, `hub/poller.go`, `hub/history.go`, tests.

**Interfaces:**

```go
// hub
type HostView struct {
	Info  model.HostInfo; Conn Conn; Snap *model.Snapshot; Age time.Duration
	Hist  *History; Procs []model.Process; Ctrs []model.Container
}
type Hub struct{ /* unexported */ }
func New(sources map[string]source.Source, interval time.Duration, publish func(id string, v HostView)) *Hub
func (h *Hub) Select(hostID, page string)   // selected host polled at interval, others every 15 s
func (h *Hub) SetVisible(hostIDs []string)   // panel rows on screen
func (h *Hub) Retry(hostID string)          // EvReset
func (h *Hub) Stop()
type History struct{ /* ring, 600 points per series */ }
func (h *History) Push(t time.Time, s model.Snapshot)
func (h *History) Series(name string) []Point // "cpu", "mem", "net.rx", "net.tx", "disk.read", "disk.write", "gpu"
type Point struct{ T time.Time; V float64 }
```

Processes are polled only while page is `processes` (and containers only on `docker`) for the selected host.

- [ ] **Step 1: Failing tests** with `source.Fake` and a fake clock:
  - selected host gets 5 polls in 10 s at 2 s; a non-selected visible host gets 1;
  - fake returns errors 3× → published `Conn.State == Offline`, next attempt after `Backoff`;
  - `HostView.Age` grows while offline and `Snap` keeps the last value;
  - `History` keeps exactly 600 points after 700 pushes, oldest dropped;
  - `source/remote` maps HTTP 401 → `EvUnauthorized`, `*transport.HostKeyError` → `EvHostKeyUnknown/Mismatch`, version mismatch → `EvIncompatible`.
- [ ] **Step 2–4 (agent-box for portable parts).** **Step 5: Commit** `feat(R1,R16,R17,R25): sources, poller tiering, history`

### D3: ActionRunner

**Files:** Create `hub/action.go`, `hub/action_test.go`.

Implements `Runner` from T0.3: `Start` takes an `Action` in `confirming` (UI already confirmed → runner moves to `running`), executes via a `func(ctx context.Context, a *Action, log func(string)) error` per kind, publishes each state change; two actions on the same `HostID+Target` cannot run at once (second → `failed` with `conflict`).

- [ ] **Step 1: Failing tests:** restart on fake source records `"jobhunter-bot:restart"` and ends `succeeded`; source error → `failed` with the `APIError` message; a package action's log lines arrive in order; concurrent same-target → second fails `conflict`.
- [ ] **Step 2–4.** **Step 5: Commit** `feat(R8,R13,R14): action runner`

## Track E — UI (Mac; Tester tests also run on agent-box)

### E1: First page — Overview on `source.Fake`

**Files:** Create `ui/app.go`, `ui/pages/overview.go`, `ui/app_test.go`.

**Interfaces:** `ui.NewApp(views func(id string) hub.HostView, hosts []store.Host, actions hub.Runner, onSelect func(hostID, page string)) *App`; `(*App).View(c *ui.Context)`; `(*App).Apply(id string, v hub.HostView)` (called inside `win.Update`).

- [ ] **Step 1: Failing test**

```go
func TestOverviewShowsCardsAndHidesAbsentHardware(t *testing.T) {
	app := testApp(hub.HostView{Snap: &model.Snapshot{CPU: model.CPU{Total: 37.5}, Battery: nil}})
	tt := ui.NewTester(app.View, 1200, 800)
	for _, want := range []string{"CPU", "37.5 %", "Memory", "Disk", "Network"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Battery") {
		t.Fatal("battery card shown without a battery")
	}
}
```

- [ ] **Step 2–4.** **Step 5: Commit** `feat(R1): overview page`

### E2: Shared base layout + components (right after the first page works)

**Files:** Create `ui/shell.go`, `ui/theme.go`, `ui/components/{card,sparkline,timechart,badge,banner,empty}.go`, `ui/shell_test.go`; modify `ui/pages/overview.go` to build only content.

Rules (CLAUDE.md "UI" adapted to native UI): one `Shell(c, app, content func())` for every page: top bar (page items left in R23 order, current host name + connection badge right), page title as the first text, offline/stale banner under the title, empty-state component, host panel slot on the right (`ui.Split`, panel 260 pt default, resizable). Theme tokens in `ui/theme.go` for light/dark (follow `mygo.Theme.IsDark()`), badge colours: green online/running, amber stale/needs confirm, red offline/error, grey paused/read-only. `timechart` always prints the unit on the y axis and the current value in the legend (R18). Icons: Lucide SVGs embedded with `//go:embed`, license file `ui/icons/LICENSE`.

- [ ] **Step 1: Failing tests:** every page key renders inside Shell with its title; hiding: with `Caps{Battery:false}` the top bar has no "Energy"; dark mode toggle via `tt.SetDark(true)` changes the card background token; `timechart` shows "%" and the latest value text.
- [ ] **Step 2–4.** **Step 5: Commit** `feat(R18,R23): shared shell layout and components`

### E3: Host panel tree, host dialog, groups, host-key dialog

**Files:** Create `ui/hostpanel/panel.go`, `ui/hostpanel/dialog.go`, `ui/hostpanel/hostkey.go`, `ui/hostpanel/panel_test.go`.

Behaviour: `ui.Tree` of groups → hosts; each host row: status dot, name, tiny "CPU 12 % · RAM 43 %"; context menu: Edit, Move to group ›, Remove (confirm); "+" button opens dialog with Connection = Tailscale/LAN (address, port) or SSH (host, port, user, key file picker) plus token field (stored via `store.Secrets`); `HostKeyUnknown` → dialog with fingerprint and "Trust" / "Cancel"; `HostKeyMismatch` → red panel text, no trust button.

- [ ] **Step 1: Failing tests** (`ui.NewTester` + fake store/secrets): add SSH host → config contains `Conn.Type=="ssh"` and secret saved under `TokenKey(id)`; `ChooseMenuItem("Move to group", "Servers")` moves it; collapsing "Servers" hides "VPS"; unknown host key shows "SHA256:" text and "Trust" calls `TrustHostKey`.
- [ ] **Step 2–4.** **Step 5: Commit** `feat(R11,R24): host panel, host dialog, host key trust`

### E4: CPU, Memory, Disk, Network pages

**Files:** Create `ui/pages/cpu.go`, `ui/pages/memory.go`, `ui/pages/disk.go`, `ui/pages/network.go`, `ui/pages/pages_test.go`.

Content: CPU = per-core bars (count = len(PerCore)), load, 5-minute `timechart` from `History.Series("cpu")`, top 10 by CPU; Memory = stacked bar (app/wired/compressed or used/cached/buffers), swap, pressure badge, top 10 by RSS, by-app list on Mac; Disk = volume table, read/write chart, top by disk I/O (rows with nil → "—"); Network = interface table, rx/tx chart, listening ports table (port, proto, process).

- [ ] **Step 1: Failing tests:** CPU page with 16-core snapshot shows 16 bars (`tt.Find("Core 16")`); Disk shows "—" for nil rates; Network lists "8765/tcp python3" from fixture processes.
- [ ] **Step 2–4.** **Step 5: Commit** `feat(R2-R5): cpu, memory, disk, network pages`

### E5: Processes page

**Files:** Create `ui/pages/processes.go`, `ui/pages/processes_tree.go`, `ui/pages/processes_inspector.go`, `ui/pages/processes_test.go`.

Content: `ui.Table` with columns from settings (name, PID, user, CPU, memory, threads, ports, started), sort by header click, search field (substring on name/command/user, `port:8765`, `user:root`), column choose/reorder/resize persisted through `store.Settings.Columns["processes"]`, tree mode via `ui.Outline` by PPID, inspector pane (command line, ports, start time, parent), Terminate / Force kill buttons → confirm dialog (Force kill needs a second confirm) → `Runner.Start(Action{Kind:"signal"})` with PID + StartTime.

- [ ] **Step 1: Failing tests:**

```go
func TestProcessesSortSearchAndKill(t *testing.T) {
	procs := fixtureProcs(3000) // pid 4242 "sleep" CPU 0, pid 77 "chrome" CPU 90
	app, runner := testAppWithProcs(procs)
	tt := ui.NewTester(app.View, 1400, 900)
	start := time.Now()
	tt.Click("Processes")
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("render took %v", time.Since(start))
	}
	tt.Click("CPU") // sort descending
	if first := tt.Texts()[firstRowIndex(tt)]; first != "chrome" {
		t.Fatalf("first row %q", first)
	}
	tt.Click("Search"); tt.Type("sleep")
	tt.Click("sleep"); tt.Click("Terminate"); tt.Click("Terminate process")
	if len(runner.Started) != 1 || runner.Started[0].Target != "4242" {
		t.Fatalf("started %+v", runner.Started)
	}
}
```

- [ ] **Step 2–4.** **Step 5: Commit** `feat(R8,R13): processes page`

### E6: Docker page + logs pane

**Files:** Create `ui/pages/docker.go`, `ui/pages/docker_logs.go`, `ui/pages/docker_test.go`.

Content: table grouped by compose project (name, image, state badge, health, CPU, memory, ports, uptime); row buttons Start/Stop/Restart (confirm, disabled while an action runs); "Logs" opens a bottom split with tail selector (100/500/1000) and Follow toggle; lines rendered as plain text in a virtualized list, capped at 5,000 lines in memory; closing the pane cancels the stream. Docker unavailable → empty state with the agent's reason.

- [ ] **Step 1: Failing tests:** `jobhunter-bot` row shows green "running"; Restart → confirm → runner gets `container` action; Logs pane shows fake lines and appends a new one when the fake channel sends; `Caps.Docker=false` shows "Docker not available on this host".
- [ ] **Step 2–4.** **Step 5: Commit** `feat(R14): docker page and logs`

### E7: Keyboard navigation

**Files:** Create `ui/shortcuts.go`, `ui/shortcuts_test.go`.

- [ ] **Step 1: Failing test:** `tt.Key(ui.Cmd, ui.Key2)` shows the CPU page title; with the VPS selected (no Energy), ⌘ numbers follow the visible order; `⌥⌘↓` selects the next host in the panel order.
- [ ] **Step 2–4.** **Step 5: Commit** `feat(R23): keyboard navigation`

### E8: GPU and Energy pages (Should)

**Files:** Create `ui/pages/gpu.go`, `ui/pages/energy.go`, tests.
- [ ] **Step 1:** failing tests: GPU page with one NVIDIA GPU shows "RTX 3080", util chart with "%" unit, VRAM bar "1.6 / 10 GB"; Energy shows charge, "Charging"/"On battery", watts, thermal state, keep-awake list. **Step 2–4.** **Step 5: Commit** `feat(R6,R7): gpu and energy pages`

### E9: Settings + i18n (Should)

**Files:** Create `ui/pages/settings.go`, `ui/i18n/i18n.go`, `ui/i18n/en.go`, `ui/i18n/vi.go`, tests.
- [ ] **Step 1:** failing tests: switching language to Vietnamese shows "Tiến trình" for the Processes tab; every key in `en` exists in `vi` (table test over the map); interval 5 s saved to config and passed to `Hub` (fake). **Step 2–4.** **Step 5: Commit** `feat(R20): settings and vietnamese`

## Track F — Mac extras (Should/Could)

### F1: Apps

**Files:** Create `mac/apps/apps.go`, `mac/apps/trash.go`, `mac/apps/apps_test.go`, `ui/pages/apps.go`.

List from `mygo.Shell.InstalledApps()` (off main thread); size = recursive `fs.WalkDir` sum (cached by bundle mtime); arch from the main executable's Mach-O/fat header; source: `/Applications/*.app` with `Contents/_MASReceipt` → App Store, path in `brew list --cask -1` output → Homebrew, else Other. Uninstall: user-writable → `mygo.Shell` trash (or `NSWorkspace.recycleURLs` via purego); not writable → macOS authorization prompt moving to `~/.Trash`; Homebrew casks offer `brew uninstall --cask <name>` instead.

- [ ] **Step 1:** failing test (Mac): copy `/System/Applications/Calculator.app` to `t.TempDir()/Test.app`, scanner over that dir lists it with size ±1 % of `du -sk`; `Trash` moves it (check `~/.Trash`), then restore. **Step 2–4.** **Step 5: Commit** `feat(R9): apps page`

### F2: Packages

**Files:** Create `mac/pkgs/brew.go`, `mac/pkgs/mise.go`, `mac/pkgs/task.go`, `mac/pkgs/testdata/*.json`, tests, `ui/pages/packages.go`.

Readers: `brew info --json=v2 --installed`, `brew outdated --json=v2`, `mise ls --json`, `mise outdated --json`; actions: `brew upgrade <name>`, `brew upgrade` (all), `brew uninstall <name>`, `brew cleanup`, `mise upgrade <tool>`, `mise prune --yes`; task runner streams combined output lines, keeps last 500, reports exit code.

- [ ] **Step 1:** failing tests on fixtures: outdated count = 3 for the fixture; task with a fake command printing 3 lines and exiting 1 → `failed`, exit code 1, last lines shown. **Step 2–4.** **Step 5: Commit** `feat(R10): homebrew and mise packages`

### F3: Menu bar item (Could)

**Files:** Create `ui/tray.go`. Tray text "CPU 12% · RAM 43%" for the selected host, click shows the main window. Proof: manual check on the Mac. Commit `feat(R21): menu bar item`

## Phase 2 details

- **I1:** `cmd/hotwatch/main.go` loads `store`, builds sources (`local.New()`, `remote.New(agentclient.New(dialer.DialAgent, token))`), starts `hub.New(..., publish)` where `publish` calls `win.Update(func(){ app.Apply(id, v) })`, logs `slog.Info("ui.first_data", "ms", …)` once and `ui.switch` on every select. Proof: `make run` shows This Mac with live values within 1 s.
- **I2 / I3 (ask the owner before each):** `make agent`, copy the binary with `scp`, run `deploy/install.sh` interactively over ssh. VPS uses `init --mode ssh`; agent-box uses `--mode tailscale`. Proof lines in REQUIREMENTS R12, R14, R15 acceptance criteria, including `curl -m 3 http://167.253.157.57:7171/v1/info` failing.
- **I4:** `scripts/measure-app.sh` and `scripts/measure-agent.sh` exactly as in ARCHITECTURE.md "Measuring the quality metric"; record results for `/dev-verify`.
- **I5:** `mygo.json` with name "Hotwatch", bundle id `dev.cicca.hotwatch`, icon from `ui/icons/app.svg` (own design); `make dmg`; write `DEMO_FLOWS.md` sections: Mac overview, add VPS via SSH, Docker restart + logs, processes terminate, Apps, Packages, every other page; `scripts/reset-demo.sh` restores a demo `config.json` (This Mac, VPS, agent-box in groups).
