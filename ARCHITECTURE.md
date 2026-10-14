# Architecture

The template's grading-oriented headings are adapted to this project: "How an answer moves" becomes the sampling flow, "Reliability under non-determinism" becomes errors and failure states, and "Measuring agreement" becomes measuring the quality metric (fast and light, R17/R25). No LLM is used anywhere.

## Scope (built, faked, left out — and why)

Built (all Must, plus Should where noted in REQUIREMENTS.md):
- **Hotwatch.app** (macOS arm64): mygo native UI. Top page bar (R23), right host panel with grouped tree (R24), pages Overview/CPU/Memory/Disk/Network/GPU/Energy/Processes/Docker/Apps/Packages (R1–R10, R13, R14), settings (R20).
- **hotwatch-agent** (Linux amd64/arm64): one static binary, systemd service, HTTP/JSON API over Tailscale with bearer token, read-only switch and audit log (R12, R15).
- **Shared Go packages** for the data model, collectors, Docker client and agent protocol.

Simplified:
- Per-process disk I/O and listening-port ownership on macOS only for processes the user may inspect. `proc_pid_rusage`/`proc_pidfdinfo` need root for other users' processes; those rows show "—" with a tooltip rather than a wrong value (R4, R5).
- History lives in memory only (last 10 minutes per host); nothing is persisted except settings and the host tree.

Left out: listed in REQUIREMENTS.md (Telegram, auto-update, Windows, remote Apps/Packages, Docker exec/images/volumes, Linux per-process traffic, signing, agent TLS, extra languages).

Every Must is covered (see "Coverage" at the end). None is uncovered; the caveat on R4/R5 is above.

## Components

```
 Hotwatch.app (Mac)                                         hotwatch-agent (VPS, agent-box)
 ┌──────────────────────────────────────────────┐           ┌──────────────────────────────┐
 │ ui/  shell: TopBar · Page · HostPanel        │           │ agent/server  auth · limits  │
 │      pages/*  components/* (card, sparkline, │           │ agent/handlers  /v1/*        │
 │      timechart, table, badge, banner)        │           │ agent/audit  JSONL           │
 │        ▲ win.Update(state)                   │  HTTP/JSON│        │                     │
 │ hub/  one Poller per host, History rings,    │◀─────────▶│ collect/linux  /proc /sys    │
 │       ConnState machine, ActionRunner        │ Tailscale │ gpu/nvidia  nvidia-smi       │
 │        │ Source interface                    │  + Bearer │ docker/  Engine API (socket) │
 │ source/local ─▶ collect/darwin, gpu/apple,   │           └──────────────────────────────┘
 │                 power/, mac/apps, mac/pkgs   │
 │ source/remote ─▶ agentclient/                │
 │ store/  config.json · Keychain tokens        │
 └──────────────────────────────────────────────┘
 model/  shared types + protocol version (imported by both sides)
```

| Package | Responsibility | Depends on |
|---|---|---|
| `model` | All wire and view types (`Snapshot`, `Process`, `Container`, `HostInfo`, `Caps`, errors), `ProtocolVersion` | stdlib |
| `collect` | `Collector` interface; `collect/darwin`, `collect/linux` implementations (CPU, memory, disks, network, load, uptime, processes, listening ports) | `model`, gopsutil (see Choice 1), `golang.org/x/sys` |
| `gpu` | `gpu/apple` (IOKit via purego), `gpu/nvidia` (`nvidia-smi --query-gpu=... --format=csv,noheader,nounits`) | `model` |
| `power` | macOS battery, power draw, thermal state, sleep assertions (IOKit/IOPM via purego) | `model` |
| `docker` | Minimal Docker Engine API client over the unix socket: list, inspect stats, start/stop/restart, logs stream (demultiplexed) | `model`, stdlib `net/http` |
| `mac/apps` | Installed apps (mygo `Shell.InstalledApps`), bundle size, architecture (Mach-O header), source detection, move to Trash | `model`, mygo |
| `mac/pkgs` | Homebrew (`brew info --json=v2 --installed`, `brew outdated --json=v2`) and mise (`mise ls --json`, `mise outdated --json`) readers; task runner with streamed log | `model` |
| `source` | `Source` interface; `source/local` (collectors in-process), `source/remote` (agent client) | `model`, `collect`, `agentclient` |
| `transport` | Per-host dialer: `Direct` (TCP to Tailscale/LAN address) or `SSH` (golang.org/x/crypto/ssh client, ssh-agent via `SSH_AUTH_SOCK` or key file, strict `known_hosts` check; dials the agent's 127.0.0.1 port through the SSH connection; one SSH connection per host, reused) | `golang.org/x/crypto/ssh` |
| `agentclient` | HTTP client for the agent protocol over a `transport` dialer: timeouts, size limits, strict JSON decode, log stream reader | `model`, `transport` |
| `hub` | One `Poller` goroutine per host, `History` ring buffers, `ConnState` machine, `ActionRunner` (confirm → run → result) | `source`, `model` |
| `store` | Host tree + settings in `~/Library/Application Support/Hotwatch/config.json` (atomic write, versioned); tokens in mygo `Secrets` (Keychain) | `model`, mygo |
| `ui` | Window shell (TopBar, content, HostPanel), pages, shared components, theme tokens, i18n (en, vi) | mygo `ui`, `ui/chart`, `hub` |
| `agent` | Server, auth middleware, handlers, sampler lifecycle, audit log, config file | `model`, `collect/linux`, `gpu/nvidia`, `docker` |
| `cmd/hotwatch`, `cmd/hotwatch-agent` | Entry points, flags, wiring | everything above |

Base layout (adapts the CLAUDE.md "UI" rules to native UI): one `ui.Shell` component builds every window: TopBar (page tabs left, host name and connection badge right), content area with an `h1`-equivalent page title, empty state and offline banner, HostPanel on the right. Pages only build the content area, so they all look the same in the demo. State badges: green = online/running, amber = stale/restarting/needs confirm, red = offline/error/exited, grey = paused/read-only.

## How a sample moves: collect → snapshot → poller → view

1. **Launch.** `cmd/hotwatch` loads `store` (host tree, settings, tokens), creates the window with `ui.View(app.view)` and starts the `hub`. The "This Mac" host always exists (kind `local`).
2. **Poll.** For each host, a `Poller` goroutine calls `Source.Snapshot(ctx)` every `interval` (default 2 s). Only the **selected host** and hosts whose panel row is visible are polled at full rate; others are polled every 15 s for the status dot and tiny CPU/RAM readout (keeps R17).
3. **Local source.** `source/local` calls `collect/darwin` (CPU ticks delta, memory via `host_statistics64`, disks, network counters delta, load, uptime), `gpu/apple`, `power`. CPU and rate values are deltas against the previous sample; the first sample returns rates as `nil` (shown as "—", never 0).
4. **Remote source.** `source/remote` calls `GET /v1/snapshot`. The agent keeps its own sampler running while any client polled in the last 30 s, so deltas are computed on the agent at its interval; with no client it sleeps (R17).
5. **Page data on demand.** Processes, containers, apps and packages are fetched only while their page is open (`Source.Processes`, `Source.Containers`, ...), at the same interval for processes/containers and on open + manual refresh for apps/packages.
6. **Hub → UI.** The poller appends to `History` (ring of 600 points per series) and calls `win.Update(func(){ app.hosts[id] = view })`. The view reads only this copied state on the main thread; no locks in the view.
7. **Actions.** A button (restart container, terminate process, update formula, uninstall app) creates an `Action` in `ActionRunner`. The UI shows a confirm dialog; on confirm the runner calls the source (`POST /v1/...` remotely), streams the result or log into the action, and refreshes the affected page.
8. **Logs.** Opening a container's logs starts `Source.ContainerLogs(ctx, id, tail, follow)`, which returns a line channel; closing the pane cancels the context and the HTTP stream.

## State machines

### Host connection (`hub.ConnState`)

| State | Meaning | On success | On failure | Shown as |
|---|---|---|---|---|
| `Connecting` | First contact or after config change; `GET /v1/info` pending | → `Online` (if protocol version matches) / → `Incompatible` | → `Offline` (network, timeout) / → `Unauthorized` (401) | grey dot, spinner |
| `Online` | Last snapshot < 2 intervals old | stay | 1st failure → `Stale` | green dot |
| `Stale` | 1–2 consecutive failures; last data kept with its age | → `Online` | 3rd failure → `Offline` | amber dot, "updated Ns ago" |
| `Offline` | ≥ 3 failures; retry with backoff 2, 4, 8, 16, 30, 60 s (cap) | → `Online` | stay, next backoff | red dot, "offline · last seen …", Retry button |
| `Unauthorized` | Agent answered 401, or SSH authentication failed | — (no retry) | — | red, "token rejected" / "SSH login failed" — edit host |
| `HostKeyUnknown` | SSH host key not in any known_hosts | → `Connecting` after the user accepts the shown SHA256 fingerprint | stay | amber, "verify host key" dialog |
| `HostKeyMismatch` | SSH host key differs from known_hosts | — (no retry) | — | red, "host key changed — possible attack"; the user must remove the old key by hand |
| `Incompatible` | Agent `ProtocolVersion` major differs | — (no retry) | — | red, "agent v… needs update" |

Editing a host's address or token, or pressing Retry, moves any state to `Connecting`. The local host is always `Online`.

### Action (`hub.Action`)

| State | Next | Notes |
|---|---|---|
| `Confirming` | `Running` (confirm) / `Cancelled` | Dialog names the target and the effect; SIGKILL needs a second confirm |
| `Running` | `Succeeded` / `Failed` | Buttons for the same target are disabled; log streams for package tasks |
| `Succeeded` | — | Toast; affected page refreshes |
| `Failed` | — | Shows the error code, message and last 20 log lines |
| `Cancelled` | — | Nothing sent |

## Data model

```go
// model (shared by app and agent)
const ProtocolVersion = "1.0"

type HostInfo struct {
    ProtocolVersion string
    AgentVersion    string
    Hostname, OS, Arch, Kernel string
    CPUCount        int
    Caps            Caps
}
type Caps struct{ GPU, Battery, Docker, Processes, Apps, Packages, ReadOnly bool }

type Snapshot struct {
    Time      time.Time
    CPU       CPU         // Total %, PerCore []float64, Load [3]float64
    Memory    Memory      // Total, Used, App, Wired, Compressed, Cached, Buffers, SwapTotal, SwapUsed uint64; Pressure string
    Disks     []Volume    // Mount, FS, Total, Used uint64; ReadBps, WriteBps *float64
    Net       []Interface // Name string; Addrs []string; RxBps, TxBps *float64; Up bool
    GPUs      []GPU       // Name; UtilPct *float64; MemUsed, MemTotal *uint64; TempC *float64
    Battery   *Battery    // nil when absent
    Uptime    time.Duration
}

type Process struct {
    PID, PPID int32; StartTime time.Time      // (PID, StartTime) identifies a process; guards PID reuse
    Name, User, Command string                // Command = argv joined; never environment
    CPUPct float64; RSS uint64; Threads int32
    DiskReadBps, DiskWriteBps *float64        // nil = not readable
    Ports []Port                              // listening TCP/UDP
    AppBundle string                          // macOS only
}

type Container struct {
    ID, Name, Image, State, Status, Health string
    Project string                            // com.docker.compose.project label
    CPUPct *float64; MemUsed, MemLimit *uint64
    Ports []string; StartedAt time.Time
}

type APIError struct{ Code, Message string } // codes: unauthorized, forbidden_read_only, not_found, bad_request, conflict, docker_unavailable, internal
```

App-side persistent state (`config.json`, versioned, written atomically via temp file + rename):

```json
{
  "version": 1,
  "groups": [{"id": "g-servers", "name": "Servers", "parent": ""}],
  "hosts": [
    {"id": "local", "name": "This Mac", "kind": "local", "group": "g-local"},
    {"id": "h-vps", "name": "VPS", "kind": "agent", "group": "g-servers",
     "conn": {"type": "ssh", "sshHost": "167.253.157.57", "sshPort": 22, "sshUser": "root", "keyPath": "", "agentPort": 7171}},
    {"id": "h-box", "name": "agent-box", "kind": "agent", "group": "g-lab",
     "conn": {"type": "direct", "address": "100.73.66.67", "agentPort": 7171}}
  ],
  "settings": {"intervalMs": 2000, "language": "vi", "columns": {"processes": ["name","pid","user","cpu","rss","threads"]}, "launchAtLogin": false},
  "ui": {"selectedHost": "h-vps", "page": "docker", "openGroups": ["g-servers"]}
}
```

Tokens are not in this file: `Secrets` key `hotwatch/host/<id>` (macOS Keychain). In-memory only: `History` rings, current snapshots, process/container lists, action log.

SSH host keys the user accepted in the app are stored in `~/Library/Application Support/Hotwatch/known_hosts` (`~/.ssh/known_hosts` is read but never written).

Agent config `/etc/hotwatch/agent.json` (mode 0600, owner `hotwatch`): `{"listen":"127.0.0.1:7171","tokenSHA256":"…","readOnly":false,"intervalMs":2000}`. The plain token is printed once by `hotwatch-agent init` and stored only in the Mac's Keychain.

## Interfaces / contracts between parts

```go
// source: what the hub and UI consume; implemented by local and remote
type Source interface {
    Info(ctx context.Context) (model.HostInfo, error)
    Snapshot(ctx context.Context) (model.Snapshot, error)
    Processes(ctx context.Context) ([]model.Process, error)
    Signal(ctx context.Context, pid int32, start time.Time, sig model.Signal) error   // SigTerm, SigKill
    Containers(ctx context.Context) ([]model.Container, error)
    ContainerAction(ctx context.Context, id string, act model.ContainerAction) error  // Start, Stop, Restart
    ContainerLogs(ctx context.Context, id string, tail int, follow bool) (<-chan model.LogLine, error)
}

// collect: one per OS
type Collector interface {
    Sample(ctx context.Context) (model.Snapshot, error) // deltas vs previous call
    Processes(ctx context.Context) ([]model.Process, error)
}
```

Agent HTTP API (`/v1`, JSON, `Authorization: Bearer <token>` on every route):

| Method & path | Body / query | Response | Action? |
|---|---|---|---|
| `GET /v1/info` | — | `HostInfo` | no |
| `GET /v1/snapshot` | — | `Snapshot` | no |
| `GET /v1/processes` | — | `[]Process` | no |
| `POST /v1/processes/{pid}/signal` | `{"signal":"TERM"\|"KILL","startTime":"RFC3339Nano"}` | `204` / `APIError` (409 `conflict` if start time differs) | yes |
| `GET /v1/containers` | — | `[]Container` | no |
| `POST /v1/containers/{id}/{start\|stop\|restart}` | — | `204` / `APIError` | yes |
| `GET /v1/containers/{id}/logs` | `tail=1..5000&follow=0\|1` | NDJSON stream of `LogLine{Time, Stream, Text}` | no |

Errors are `APIError` with HTTP 400/401/403/404/409/502/500. Mac-only features (`mac/apps`, `mac/pkgs`, `power`) are called by `source/local` directly and are not part of the agent API.

Test fakes (Phase 0, used by every track): `source.Fake` (scripted snapshots, errors, latency), `agent` run in-process on `httptest.Server`, `docker.FakeEngine` (httptest server speaking the Engine API subset over a temp unix socket), `/proc` and `/sys` fixture trees under `collect/linux/testdata/`, `brew`/`mise` JSON fixtures.

## Errors and failure states

| Failure | Detection | Behaviour |
|---|---|---|
| Agent unreachable / timeout (3 s per request) | `Source` error | ConnState → Stale → Offline with backoff; last data stays, labelled with its age (R16) |
| Wrong token | 401 | `Unauthorized`; no retries (avoids hammering); edit-host prompt |
| Agent protocol major mismatch | `/v1/info` | `Incompatible`; banner names both versions |
| Malformed or oversized response | strict decode (`DisallowUnknownFields` off for forward compat, but type errors fail), size caps: snapshot 1 MB, processes 8 MB, containers 2 MB | treated as a failed poll; error logged with host and route |
| Docker socket missing / permission denied | agent Docker client | `Caps.Docker=false` in info, Docker page shows "Docker not available on this host: <reason>" |
| Container action fails (e.g. already stopped) | Engine API status | `Action.Failed` with Docker's message |
| PID reused before signal | start-time mismatch | 409 `conflict`; UI refreshes processes and says the process already ended |
| Signal not permitted | `EPERM` | `Action.Failed`: "not permitted (process owned by root)" |
| `nvidia-smi` missing or hangs | exec with 2 s timeout | `Caps.GPU=false` / GPU card shows last value with age |
| `brew`/`mise` command fails | exit code ≠ 0 | `Action.Failed` with exit code + last 20 lines; outdated badges unchanged |
| config.json corrupt | JSON decode at start | rename to `config.json.bad-<time>`, start with defaults, show a banner |
| Keychain locked / denied | `Secrets` error | host shows `Unauthorized`-style "token unavailable" state |
| Collector partial failure (one disk unreadable) | per-field error | field becomes nil ("—"); rest of the snapshot still shown |
| Main-thread stall risk | — | all I/O off the main thread; view reads copied state only; `mygo vet` in CI |

## Security

- **Network exposure**: agent binds only to the address in `listen`. `hotwatch-agent init --mode tailscale` fills it with `tailscale ip -4`; `--mode ssh` (default when Tailscale is absent) uses `127.0.0.1`. Any other address (public, `0.0.0.0`) is refused; there is no override flag. Acceptance check (R15): `curl` to the VPS public IP:7171 fails.
- **SSH transport**: host keys checked against `~/.ssh/known_hosts` and the app's own file; an unknown key requires the user to accept its SHA256 fingerprint; a changed key is a hard stop. Keys come from ssh-agent (`SSH_AUTH_SOCK`, which macOS sets for GUI apps) or a key file the user picks; passphrases are asked for, never stored. Only `direct-tcpip` channels to `127.0.0.1:<agentPort>` are opened: no remote commands, no shell.
- **Authentication**: 32-byte random token (base64url) per host; agent stores only its SHA-256 and compares with `subtle.ConstantTimeCompare`. 5 failed auths per minute per source IP → 1-minute lockout (logged).
- **Authorization**: `readOnly` rejects every `POST` with 403 `forbidden_read_only` before any work.
- **Least privilege**: agent runs as system user `hotwatch`, member of `docker` (needed for R14; root-equivalent, accepted in Q3). systemd unit: `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=read-only`, `PrivateTmp=yes`, `AmbientCapabilities=CAP_KILL` (needed to terminate other users' processes, Q4), `CapabilityBoundingSet=CAP_KILL`.
- **Untrusted input (agent side)**: path params validated (`pid` int32 > 1; container `id` matches `^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$` and must exist in the current container list); `tail` clamped 1..5000; request bodies ≤ 4 KB; server timeouts (read 5 s, write 10 s, logs stream exempt but cancelled on client disconnect).
- **Untrusted input (app side)**: agent responses are data, never code; sizes capped; strings displayed as plain text; numbers clamped to sane ranges before drawing. External commands (`brew`, `mise`, `nvidia-smi`) run with an argv list, never through a shell; formula/tool names come from the tools' own JSON output and are passed as single argv items.
- **Secrets**: process environments are never read or sent. Container logs may contain secrets: shown in the pane only, never written to disk or kept after the pane closes. Tokens never logged; audit log records time, source IP, route, target, result.
- **Admin on the Mac**: uninstalling an app the user cannot write asks macOS for admin through the system authorization prompt; hotwatch never stores the password.
- **Clean-room (R19)**: implementation sources are OS documentation (Apple IOKit/libproc/sysctl man pages, Linux `proc(5)`, Docker Engine API v1.45 docs, nvidia-smi docs) and mygo docs. Pulse's decompiled output is never opened during development; icons from Lucide (ISC license).

## Measuring the quality metric (fast and light: R17, R25)

- `scripts/measure-app.sh` (Mac): launches Hotwatch with 3 hosts configured, waits 30 s, samples `ps -o %cpu=,rss= -p <pid>` every second for 60 s, prints mean CPU % and max RSS MB. Pass: CPU < 2 %, RSS < 150 MB.
- `scripts/measure-agent.sh` (run over ssh on VPS and agent-box): same sampling for `hotwatch-agent` while one app polls at 2 s, and again with no client (should drop to ~0 %). Pass: CPU < 1 %, RSS < 30 MB.
- R25: the app logs `ui.first_data` (ms since process start) and `ui.switch` (page/host switch → frame drawn) with `slog`; `scripts/measure-app.sh` parses them. Pass: first data < 1000 ms; p95 switch < 100 ms over 50 scripted switches (driven through mygo's `Tester` in a benchmark, plus a manual check in the real window).
- Go benchmarks for hot paths: `collect/linux` parse of `/proc` fixtures, `model` JSON encode/decode of a 500-process list, `hub.History` append.
- All test suites and Linux benchmarks run on agent-box (`/cicca-run`); darwin collector tests and the app measurement run on the Mac because they need macOS.

## Trade-offs and what's next

### Choice 1: how to read system metrics
- **A. gopsutil v4 for portable metrics + small own code for Apple GPU, battery, thermal and Mac app data (recommended).** Mature, BSD-3 licensed, covers CPU/memory/disk/net/process on darwin and linux; less code to own. Risk: some darwin calls may need cgo or be slow (`process` package spawns per-PID syscalls). Phase 0 spike: build `GOOS=darwin CGO_ENABLED=0` and benchmark a 600-process scan; fall back to own `libproc` calls via purego for the process list if > 50 ms.
- **B. Own collectors everywhere** (sysctl/libproc via purego on Mac, `/proc` parsing on Linux). Smallest binary and full control; much more code and more edge cases to test.

### Choice 2: how data gets from agent to app
- **A. App polls `GET /v1/snapshot` at the interval over a kept-alive HTTP connection; logs are the only stream (recommended).** Stateless, easy to test with httptest, failure detection is natural (a missed poll), agent sleeps when nobody asks.
- **B. Agent pushes a long-lived stream (SSE/WebSocket) of snapshots.** Lower latency and fewer requests; needs reconnect logic, heartbeat and backpressure on both ends, and makes "offline" detection depend on heartbeats.

### Choice 3: topology and transport for remote hosts
The app talks to each agent directly (no central server). Per host, the owner picks the transport: **direct** over Tailscale/LAN, or **SSH tunnel** for hosts with only a public IP (owner, B9). For SSH hosts:
- **A. SSH tunnel to an agent on 127.0.0.1 (recommended).** No new public port, reuses the SSH access the owner already has, encryption and host authentication from SSH; costs one kept-open SSH connection per host and an `x/crypto/ssh` dependency.
- **B. Agent listens on the public IP with TLS (self-signed, fingerprint pinned in the app).** No SSH dependency; but opens a scanned public port, needs certificate handling and firewall/rate-limit care.

Rejected alternative for topology:
- **Central collector on agent-box** aggregating all agents, the app talking only to it. Allows history while the Mac sleeps and one place for tokens; adds a service to run, a single point of failure, and a second hop for Docker actions.

### Other trade-offs taken
- mygo native UI instead of a webview frontend (owner Q6): lower memory and instant start; we build our own cards/sparklines on `ui/chart` instead of using web chart libraries.
- History in memory only (10 min): enough for the CPU page's 5-minute chart (R2); persistent history is "what's next" (would pair with a central collector).
- Polling rate tiering (selected host at full rate, others at 15 s) trades freshness of background hosts for R17.

### What's next (not in v1)
Persistent history and alerts (needs a central collector), remote Apps/Packages for Linux (apt), Docker images/volumes, Windows build, signed auto-updates (mygo supports them).

## Coverage of Must requirements

| Req | Covered by |
|---|---|
| R1 Overview | `collect/*`, `gpu/*`, `power`, `ui/pages/overview` |
| R2 CPU | `collect/*` per-core + load, `hub.History`, `ui/pages/cpu` (time chart) |
| R3 Memory | `collect/*` memory, `ui/pages/memory` |
| R4 Disk | `collect/*` volumes + I/O (caveat: other users' processes on Mac) |
| R5 Network | `collect/*` interfaces + listening ports (same caveat) |
| R8 Processes | `Collector.Processes`, `ui/pages/processes` (`ui.Table` columns/resize/reorder, tree via `ui.Outline`), `ActionRunner` |
| R11 Hosts | `store`, `transport` (direct / SSH), `ui/hostpanel`, `hub` ConnState incl. host-key states |
| R12 Agent | `agent`, `collect/linux`, `gpu/nvidia`, `docker`, systemd unit + install script |
| R13 Remote processes | `source/remote` + `POST /v1/processes/{pid}/signal` with start-time guard |
| R14 Docker | `docker` client, `/v1/containers*`, `ui/pages/docker` with logs pane |
| R15 Agent security | Security section; tests: 401, 403 read-only, address refusal |
| R16 Failures | ConnState machine, Errors table |
| R17 Light | polling tiers, sleeping agent sampler, measurement scripts |
| R18 Improved UI | `ui.Shell`, shared components, theme tokens, sparklines, clear charts with unit + current value |
| R19 Clean-room | Security → Clean-room; own icons (Lucide) and name |
| R22 Build/ship | mygo bundle for `.app`/`.dmg`, `GOOS=linux` agent builds, install script, tests on agent-box |
| R23 Top bar | `ui.Shell` TopBar with caps-based hiding and ⌘1…⌘9 |
| R24 Host tree | `ui/hostpanel` (`ui.Tree`), `store` groups, "Move to group" menu |
| R25 Fast | cached state per host, I/O off main thread, timing logs |
