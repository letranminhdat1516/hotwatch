# Hotwatch

A lightweight system monitor for this Mac and for remote Linux hosts, built
on [mygo](https://github.com/egoist/mygo) (MIT) with native UI and no cgo.
It covers the same ground as Pulse — overview, CPU, GPU, memory, disk,
network, energy, processes, apps and Homebrew/mise packages — written from
scratch (clean-room, R19), and adds remote hosts over Tailscale or an SSH
tunnel with their processes and Docker containers.

- `cmd/hotwatch` — the Mac app.
- `cmd/hotwatch-agent` — the Linux agent (static binary, systemd unit in `deploy/`).
- `cmd/hwshot` — renders any page to a PNG from fixed data, for review.

## Setup

Requirements: Go 1.27.1 (`go.mod`), macOS 26 on Apple silicon for the app,
Linux amd64/arm64 for the agent. Nothing else: no cgo, no Node, no Xcode.

```sh
git clone <repo> hotwatch && cd hotwatch
go mod download
```

Optional on the Mac: Homebrew and/or mise (Homebrew/mise tabs), Docker on
remote hosts (Docker page).

### Agent on a Linux host

```sh
make agent                                   # dist/hotwatch-agent-linux-{amd64,arm64}
scp dist/hotwatch-agent-linux-amd64 deploy/install.sh deploy/hotwatch-agent.service root@HOST:/tmp/
ssh -t root@HOST 'cd /tmp && mv hotwatch-agent-linux-amd64 hotwatch-agent && sh install.sh tailscale'   # or: ssh
```

`install.sh` prints every step and asks before each. In `tailscale` mode the
agent listens only on the host's Tailscale address; in `ssh` mode only on
127.0.0.1, and the app reaches it through an SSH tunnel. The installer prints
a token once: paste it into the app's Add host dialog. The host keeps only
its SHA-256; the app keeps the token in the macOS Keychain (service
`Hotwatch`).

## Run

```sh
make app                                     # dist/darwin-arm64/Hotwatch.app and Hotwatch 0.1.0 arm64.dmg
open dist/darwin-arm64/Hotwatch.app
make run                                     # quick, unsigned: go run ./cmd/hotwatch
```

`make app` signs with the identity in `mygo.json` (an Apple Development
certificate; change `macos.signingIdentity` to yours). A signed app keeps
one identity across builds, so the Keychain asks for the host tokens once:
answer "Always Allow". Unsigned `make run` builds are a new program to the
Keychain each time and ask again. Settings live in
`~/Library/Application Support/Hotwatch/config.json`; the refresh interval
defaults to 1 s (Settings → Updates).

Review any page without the app:

```sh
go run ./cmd/hwshot /tmp/memory.png memory 0 - dark    # overview cpu memory gpu disk network energy apps about …
HOTWATCH_CPUPROFILE=/tmp/cpu.prof ./dist/hotwatch-app   # profiles the first 60 s
```

## Test

```sh
go vet ./...
go test ./...                                   # on the Mac: every package
scripts/box.sh --docker 'go test ./...'         # on agent-box: Linux, in golang:1.27
scripts/box.sh --docker 'go test -tags dockerint -run Integration -v ./docker'   # real Docker lifecycle
```

The Linux process-scan budget (`TestProcesses3000Budget`) runs on Linux only;
elsewhere it skips with its reason.

## Requirement traceability

Checked on 2026-10-15 at the commit that adds this table. Every result below
is from a run in that session. "Live" is the real app on this Mac with three
hosts: This Mac, agent-box over Tailscale, and the VPS over an SSH tunnel.

Suite: `go test -count=1 ./...` on the Mac → **33 ok, 4 without tests, exit 0**.
`scripts/box.sh --docker 'go vet ./... && go test -count=1 ./...'` on agent-box
→ **28 ok, exit 0**. `go vet ./...` clean, `gofmt -l .` empty.

| ID | Requirement | Code | Test | Status | Evidence |
|---|---|---|---|---|---|
| R1 | Overview cards (CPU, memory, disk, network, GPU, battery), each opening its page | `ui/pages/overview.go` | `TestOverviewCardsFollowTheReference`, `TestOverviewCardsOpenTheirPages`, `TestOverviewAnalyticsRow` | ✅ | suite green; live window captured (CPU 17 % at 41 °C, memory 16.7 GB, battery 100 % charging) |
| R2 | CPU: total, per core, load, history, top processes | `ui/pages/cpu.go`, `collect/darwin/cpu.go`, `mac/sensors` | `TestCPUPage*` (5) | ✅ | suite green; cores split 4 P + 6 E as on the M5 |
| R3 | Memory: App/Wired/Compressed/Cached, swap, pressure, by app | `ui/pages/memory.go`, `collect/darwin/mem.go` | `TestMemory*` (8), `TestMemoryMatchesActivityMonitor` | ✅ | within 3 % of `vm_stat` read before and after the sample; 5/5 alone and in the full suite |
| R4 | Disk: volumes, rates, totals since startup, top I/O | `ui/pages/disk.go`, `collect/darwin/disk.go` | `TestDisk*` (4), `TestParseImageDisks`, `TestParseAPFSVolumeNames` | ✅ | real sample: "Macintosh HD", 6.24 TB read and 4.11 TB written since startup, disk image "Pulse" (hfs) |
| R5 | Network: interfaces, addresses, rates, listening ports, per-process traffic | `ui/pages/network.go`, `collect/darwin/net.go`, `netkind.go`, `nettop.go` | `TestNetwork*` (6), `TestParseNettopKeysByPID`, `TestInterfaceKind` | ✅ | real sample: en0 Wi-Fi, utun8 VPN 100.123.1.44; one nettop run ≈ 10 ms |
| R6 | GPU (Apple IOKit; NVIDIA on Linux) | `ui/pages/gpu.go`, `gpu/apple`, `gpu/nvidia` | `TestGPU*` (8), `TestFromStatsEngines`, `TestReadAppleGPU` | ✅ | real sample: Apple M5 70 %, renderer 70 %, tiler 47 % |
| R7 | Energy: charge, flow, health, adapter, whole computer, temperatures, keeping awake | `ui/pages/energy.go`, `power/` | `TestEnergy*` (8), `TestAdapterAndSystemPower`, `TestReadBatteryMatchesPmset` | ✅ | real sample: adapter 94 W "140W USB-C Power Adapter" |
| R8 | Processes: table, columns, tree, inspector, terminate/kill | `ui/pages/processes*.go` | `TestProcesses*` (36) | ✅ | suite green; live: 651 processes on the Mac |
| R9 | Apps: size, kind, source, running, uninstall to Trash | `ui/pages/apps.go`, `mac/apps` | `TestApps*` (5), `TestUninstallExecutorTrashesOnlyLocalBundles` | ✅ | hwshot over the real scan: 78 apps, 2 running, 1 update |
| R10 | Homebrew/mise: outdated, update one/all, remove, prune, live log | `ui/pages/packages.go`, `mac/pkgs` | `TestPackages*`, `TestParseBrewInfo`, `TestAppsShowHomebrewUpdates` | ✅ | real `brew info`: mongodb-compass 1.49.2 → 1.53.0 shown; no upgrade was run in this check |
| R11 | Add/edit/remove remote hosts, direct or SSH tunnel | `ui/hostswitch*`, `source/`, `transport/` | `TestAddDirectHostDefaultsPort`, `TestAddSSHHostSavesConfigAndToken` | ✅ | live config: agent-box direct 100.73.66.67:7171; VPS over SSH to 100.87.226.98 |
| R12 | Linux agent: static binary, systemd, metrics, processes, Docker | `cmd/hotwatch-agent`, `agent/`, `collect/linux`, `deploy/` | agent and collector suites on agent-box | ✅ | `systemctl is-active` → active on both hosts; amd64 11.7 MB and arm64 10.8 MB built on agent-box |
| R13 | Remote processes with terminate | `ui/pages/processes*.go`, `agent/signal*.go` | `Test*Signal*` (13) | ⚠️ | live: VPS selected, 332 processes listed; terminating a real VPS process was not run (needs the owner's approval) |
| R14 | Docker: list, start/stop/restart, logs | `docker/`, `ui/pages/docker*.go` | `TestDocker*` (28), `TestIntegrationDockerLifecycle` | ✅ | `-tags dockerint` against agent-box's Docker: PASS in 21 s, no `hw-test-` container left |
| R15 | Agent binds Tailscale/127.0.0.1 only, token, read-only | `agent/auth.go`, `agent/config.go` | `TestAuthAndReadOnly` and 17 more | ✅ | `ss -ltnp`: agent-box 100.73.66.67:7171, VPS 127.0.0.1:7171 |
| R16 | Failures never break the UI; age and backoff | `hub/connstate.go`, `hub/poller.go` | `TestBackoff` and 18 more | ✅ | suite green; live footer "Up 15d 23h · 0s ago" |
| R17 | App < 2 % CPU and < 150 MB; agent < 1 % CPU and < 30 MB | — | measured over 60 s | ⚠️ | app at 1 s refresh, 3 hosts, VPS selected: 0.96 s CPU in 60 s = **1.6 %**, RSS **149 MB** (at the limit). Agent RSS 14.3 MB (agent-box), 12.4 MB (VPS). Agent CPU (of one core): agent-box **1.3 %**, VPS **4.0 %** — above target |
| R18 | Own look, light/dark, sparklines, one card system | `ui/components`, `ui/theme.go`, `ui/pages/widgets.go` | `TestThemeTokensMatchTheApprovedDesign`, `TestDarwinDrawsIconsUpright` | ✅ | pages rendered with hwshot and compared with the owner's screenshots; light mode not reviewed in this check |
| R19 | Clean-room | whole repo | — | ✅ | `git ls-files \| grep -i pulse` → nothing; pages rebuilt from screenshots; icons are Lucide (ISC) or SF Symbols drawn by macOS at run time |
| R20 | Settings: interval, language, columns, launch at login | `ui/pages/settings.go`, `store/`, `ui/i18n` | `TestSettingsPageChangesIntervalAndUnits`, `TestIntervalDefaults`, `TestEveryEnglishKeyHasVietnamese` | ⚠️ | interval, units and columns work; the Vietnamese catalogue is complete but pages are not switched to it; no launch at login |
| R21 | Menu bar item | `ui/nook` (notch bar instead) | `TestNookFeedAndCommands` | ⚠️ | the notch bar and island show CPU and memory; there is no menu bar status item |
| R22 | Build and ship: `.app`/`.dmg`, agent binaries, installer | `Makefile`, `mygo.json`, `resources/icon.png`, `deploy/` | — | ✅ | `make app` → Hotwatch.app 15.0 MB and Hotwatch 0.1.0 arm64.dmg 4.8 MB; `codesign --verify --deep --strict` OK, Identifier dev.hotwatch.app; agent binaries for amd64 and arm64 built on agent-box |
| R23 | Navigation across the top | `ui/shell.go` | `TestShell*` (9) | ⚠️ | replaced at the owner's request by a Pulse-style left sidebar |
| R24 | Host panel on the right | `ui/shell.go` host switcher | `TestHostSwitch*` (4) | ⚠️ | replaced with R23: hosts are a switcher at the foot of the sidebar |
| R25 | First data < 1 s; page or host switch < 100 ms | `cmd/hotwatch/wire.go` | `TestTimings` | ✅ | app log: `ui.first_data ms=390`; 11 `ui.switch` entries, the slowest 11 ms |

## Not done / next

- **Agent CPU on the VPS is 4 %** of one core against a 1 % target. Profile the
  agent's sampler on the 4-core VPS and cut its per-poll work.
- **Distribution:** the app is signed with an Apple Development certificate,
  which runs on this Mac; other Macs need a Developer ID and notarisation.
- **Vietnamese (R20):** the catalogue is complete but pages still print
  English strings directly. Launch at login is missing.
- **Menu bar item (R21)** is not built; the notch bar covers part of it.
- **Remote terminate (R13)** was not tried on a real VPS process in this check.
- **App memory** sits at the 150 MB limit at a 1 s refresh.
- **Light mode** of the rebuilt pages was not reviewed. Selected table rows use
  the rose accent where Pulse uses blue.
- The Packages sidebar item duplicates the Homebrew/mise tabs now on Apps.
- The final whole-branch review and the PR to `main` are not done.
