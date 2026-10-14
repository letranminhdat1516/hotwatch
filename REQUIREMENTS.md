# Requirements

hotwatch: a lightweight system monitor for the owner's own machines. A Mac desktop app (native UI on egoist/mygo, MIT) shows the local Mac and any remote hosts the owner adds (e.g. the VPS). A small agent on each remote host reports metrics, processes and Docker containers over Tailscale.

Brief lines (from CLAUDE.md Project context and the owner's messages):
- B1 "lightweight multi-host system monitor on MIT egoist/mygo (Go)" / "xài mygo làm cho nhẹ"
- B2 "clean-room feature parity with Pulse (overview, CPU, GPU, memory, disk, network, energy/battery, processes, apps, Homebrew/mise) written from scratch, NOT translated from Pulse's decompiled code or assets" / "giữ toàn bộ chức năng hiện tại"
- B3 "improved UI" / "cải thiện UI hiện tại trước"
- B4 "user-added remote hosts (VPS) over Tailscale" / "máy vps của tao mà tao add vào"
- B5 "process view" / "coi xem cái process"
- B6 "Docker management (list/start/stop/restart/logs)" / "quản lí đc docker đang chạy gì"
- B7 "No Telegram"
- B8 "heavy builds/tests run on agent-box per /cicca rules"; "Ship target: private GitHub repo … Mac .app + Linux agent binary"

| ID | Requirement | Source (brief line) | Acceptance criterion | MoSCoW | Fake/simplify? |
|---|---|---|---|---|---|
| R1 | Overview page for the selected host: CPU %, memory used/total, disk used/total, network in/out rate, load average, uptime, plus GPU and battery cards when the host has them | B2 | With the Mac selected, every card shows a live value that changes within 2 refresh intervals; cards for absent hardware (no battery, no GPU) are hidden, not shown as 0 | Must | No |
| R2 | CPU page: total and per-core usage, load average, 5-minute history chart, top processes by CPU | B2 | Per-core bars count equals `sysctl -n hw.ncpu` on the Mac and `nproc` on the VPS; history chart shows ≥ 5 min of samples after 5 min open | Must | No |
| R3 | Memory page: used, app, wired, compressed (Mac), cached/buffers (Linux), swap, memory pressure, top processes by memory, memory grouped by app (Mac) | B2 | Used + free figures are within 5% of `vm_stat` (Mac) / `free -b` (Linux) taken at the same time | Must | No |
| R4 | Disk page: each mounted volume with capacity/used/free, read/write rates, top processes by disk I/O | B2 | Volumes listed match `df -h` real filesystems (no tmpfs/overlay noise); rates move while `dd` writes a 1 GB file | Must | Per-process disk I/O on Linux simplified to `/proc/<pid>/io` when readable |
| R5 | Network page: interfaces with addresses and in/out rates, listening ports with owning process, per-process traffic | B2 | A `python3 -m http.server 8765` shows up as a listening port owned by `python3`; `curl` of a large file raises the active interface rate | Must | Per-process traffic is Should on Mac (via `nettop`) and left out on Linux |
| R6 | GPU page: utilization and memory where available (Apple Silicon via IOKit; NVIDIA via `nvidia-smi` on Linux hosts such as agent-box) | B2, Q2 | On the Mac a GPU-heavy video shows utilization > 0; on agent-box the RTX 3080 utilization/VRAM match `nvidia-smi` within 5% | Should | No GPU card on hosts without a supported GPU |
| R7 | Energy page (Mac): battery charge, charging state, time remaining, power draw in watts, temperatures, thermal state, which apps keep the Mac awake | B2 | Unplugging the charger flips the state within 2 intervals; battery % matches `pmset -g batt` | Should | Temperatures shown only for sensors the OS exposes without root |
| R8 | Processes page: sortable, filterable table (name, PID, user, CPU, memory, threads, ports, started); choose/reorder/resize columns; tree view (parent/child); inspector with command line and open ports; quit/force-quit with confirmation | B2, B5 | Sorting by CPU puts the busiest process first; search `chrome` narrows rows; quitting a test `sleep 1000` process removes it after confirm; column choice survives restart | Must | Group-by-app on Mac only; environment variables never shown |
| R9 | Apps page (Mac): installed apps with size, architecture, source (App Store / Homebrew cask / other), version; uninstall by moving to Trash with confirmation, asking for admin only when the bundle is not user-writable | B2 | A test app copied into `~/Applications` is listed with the right size and is in the Trash after uninstall | Should | No |
| R10 | Packages page (Mac): Homebrew formulae and casks plus mise tools — installed, outdated, update one/all, remove, prune unused versions, live log of the running task | B2 | `brew outdated` count matches the "outdated" badge; updating one formula streams its log and the badge decrements on success; a failing command shows its exit code and last log lines | Should | Only on hosts where `brew`/`mise` are installed |
| R11 | Add, edit and remove remote hosts (name, Tailscale address, port, token); hosts listed in a sidebar switcher with online / offline / last-seen status; persisted locally | B4 | Adding the VPS by its Tailscale IP shows it online within 5 s; stopping its agent turns it offline with a "last seen" time; host list survives app restart | Must | No |
| R12 | Remote agent for Linux (amd64 and arm64): one static binary, runs as a systemd service, reports R1–R5 metrics, processes and Docker data; GPU via `nvidia-smi` when present | B4, B5 | `hotwatch-agent` on the VPS answers an authenticated request with metrics whose CPU count equals `nproc`; the service restarts after `kill -9` | Must | No |
| R13 | Remote processes: the same Processes page (R8) for a remote host, including terminate with confirmation | B5, Q4 | Selecting the VPS lists its processes; terminating a test `sleep 1000` on the VPS removes it after confirm | Must | No grouping by app on Linux; SIGTERM first, SIGKILL only after a second confirm |
| R14 | Docker page per host: containers (name, image, state, health, CPU %, memory, ports, uptime, compose project); start/stop/restart with confirmation; logs with tail N and live follow | B6 | On the VPS `jobhunter-bot` is listed as `running`; restarting it via the app resets its uptime; the logs pane shows the same last 100 lines as `docker logs --tail 100 jobhunter-bot` and follows new lines | Must | Image/volume/network management and exec shells are left out |
| R15 | Agent security: listens only on the Tailscale address (or localhost), requires a per-host bearer token (constant-time compare), read-only mode switch that rejects every action, every action written to an audit log | B4, B6 | A request with no or a wrong token gets 401; with `--read-only` a restart request gets 403 and the container keeps running; the agent is unreachable on the VPS public IP | Must | Token over Tailscale (WireGuard-encrypted), no extra TLS layer |
| R16 | Connection failures never break the UI: stale data is marked with its age, reconnects use backoff, action errors show the agent's message | B4 | Blocking the agent port mid-session shows "offline · last seen Ns ago" and the app stays responsive; unblocking recovers without restart | Must | No |
| R17 | Lightweight: desktop app idle CPU < 2 % and RSS < 150 MB at 2 s refresh with 2 hosts; agent idle CPU < 1 % and RSS < 30 MB | B1 | Measured with `ps -o %cpu,rss` averaged over 60 s on the Mac and the VPS; numbers recorded in `/dev-verify` output | Must | No |
| R18 | Improved UI over Pulse: own visual identity (own name, icons from an open-licensed set, palette), light/dark following the system, sparkline history on every metric card, one consistent card/table system across pages, keyboard navigation between pages; charts stay as clear as Pulse's: labelled axes with units, current value printed next to each series, readable at a glance | B3, Q1 | Every page uses the same card and table components; toggling macOS appearance switches theme live; each overview card shows a sparkline; every chart shows its unit and current value without hovering | Must | No |
| R19 | Clean-room: no code, strings, icons or other assets copied or translated from Pulse or its decompiled output; features are re-implemented from public OS documentation | B2 | Repository contains no file derived from `~/Downloads/Pulse.0.1.14-decompiled/`; a reviewer finds no Pulse branding | Must | No |
| R20 | Settings: refresh interval (1/2/5 s), UI language (English and Vietnamese), visible columns, launch at login; persisted | B2 | Setting the interval to 5 s gives 12 samples per minute instead of 30 (counted in the app log); settings survive restart | Should | Only English and Vietnamese |
| R21 | Menu bar item showing CPU and memory of the selected host, opening the app on click | B2 | Menu bar text updates every interval; clicking it brings the main window forward | Could | No |
| R22 | Build and ship: Mac arm64 `.app` (and `.dmg`) via mygo tooling; Linux agent binaries for amd64/arm64; install script + systemd unit for the agent; tests and builds run on agent-box | B8 | `go test ./...` passes on agent-box; the `.app` launches on the Mac; the agent installs on the VPS with one command after the owner approves | Must | Signing/notarization left out (personal use) |
| R23 | Page navigation is a horizontal bar across the top of the window (Overview, CPU, Memory, Disk, Network, GPU, Energy, Processes, Docker, Apps, Packages), replacing a left sidebar; pages a host does not support are hidden for that host | B3, Q1 | Clicking each item switches page; selecting the VPS hides Energy/Apps/Packages; ⌘1…⌘9 jump to the first nine items | Must | No |
| R24 | Host panel on the right side of the window: hosts arranged in a tree of user-named groups (e.g. Local › This Mac, Servers › VPS, Lab › agent-box), groups collapsible, each host showing online status and a tiny CPU/RAM readout; add, rename, move between groups, remove | B4, Q1 | Creating group "Servers" and moving the VPS into it persists across restart; collapsing a group hides its hosts; selecting a host switches every page to it | Must | Nested groups allowed; drag-and-drop replaced by a "Move to group" menu |
| R25 | Fast: window shows first data in < 1 s after launch; switching page or host renders in < 100 ms with cached data | B1, Q5 | Timed from app log timestamps on the Mac with 3 hosts added | Must | No |

## Faked / simplified / left out (and why)
- **Telegram / any notifications to external services**: owner said no (B7).
- **Auto-update**: personal app in a private repo; rebuild and reinstall instead.
- **Windows support**: not requested; mygo supports it but nobody will test it.
- **Apps and Packages pages for remote hosts**: the brief puts remote focus on processes and Docker; Linux package managers differ per distro.
- **Docker beyond containers** (images, volumes, networks, `exec` shells, compose editing): the brief lists list/start/stop/restart/logs only; `exec` would add a remote shell, a large security surface.
- **Per-process network traffic on Linux**: needs root or eBPF; not worth it for v1.
- **Code signing and notarization**: personal use; the app runs after a one-time Gatekeeper override.
- **TLS on the agent**: traffic stays inside Tailscale (WireGuard-encrypted); token auth on top.
- **Languages beyond English and Vietnamese**: the owner's languages; Pulse ships more.
- **Mac as a remote host**: the desktop app reads the Mac directly; no agent on the Mac.

## Answered questions (owner, 2026-10-14)
- **Q1 UI**: all proposed improvements; keep charts as clear as Pulse's; page navigation becomes a top bar (R23); host picker becomes a right-side panel with hosts grouped by management level (R24).
- **Q2 hosts**: monitor both the VPS and agent-box (GPU via `nvidia-smi`).
- **Q3 Docker access**: agent runs as a dedicated user in the `docker` group, with the read-only switch.
- **Q4 remote kill**: allowed (R13, now Must).
- **Q5 performance**: "fast and light": R17 targets accepted, plus R25 responsiveness.
- **Q6 / Q7**: mygo native UI; app name "Hotwatch".

## Remaining assumptions
- "Menu bar" in Q1 is read as an in-window top navigation bar (R23), not the macOS system menu bar; the system menu bar item stays the optional R21.
- Go 1.27+ (mygo requirement; the Mac has go1.27.0).
- The CLAUDE.md "UI" section targets web pages; ARCHITECTURE.md adapts its intent (shared layout, state badges, empty states, plain labels) to mygo native UI.
