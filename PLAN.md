# Hotwatch Implementation Plan — v2 (remaining work after the per-tab audit)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish hotwatch to screen-by-screen parity with the reference monitor on the Mac (dot charts on every page, app icons, Apps, Packages, About, menu bar, Vietnamese) in the owner's own red-pink visual identity, measured light and fast, and shipped as a `.app`/`.dmg` with the Linux agent.

**Architecture:** Unchanged from v1 (ARCHITECTURE.md): `model` types; per-OS `collect`; Linux `agent` behind a bearer token; the app's `hub` polls one `Source` per host and pushes copied state to the native `ui`. v2 adds three Mac-only data packages (`mac/appicon`, `mac/apps`, `mac/pkgs`) plus `mac/exec` (the single place that runs external commands), an `ui/i18n` string table, and a token pass driven by a Claude Design mockup the owner approves first.

**Tech Stack:** Go 1.27, github.com/egoist/mygo (native `ui`, `ui/chart`, tray), github.com/ebitengine/purego (IOKit/CoreFoundation/AppKit via `internal/cf`), gopsutil v4, Lucide icons (ISC). No cgo anywhere.

**Spec:** REQUIREMENTS.md (R1–R25), ARCHITECTURE.md, UI-DESIGN.md (tokens, card anatomy, page references captured from the running reference app, data gaps), and the approved Claude Design mockup (its link goes into UI-DESIGN.md "Tokens" on approval).

## Done in v1 (branch `feat/v1`, up to 5d2e14b) — do not redo

| Area | Commits | What exists |
|---|---|---|
| Contracts, collectors, agent, transport, hub | T0.1 … D3 | model, source/collect fakes, ConnState, store, docker fake+client, agent HTTP + signals, SSH tunnel, poller tiers, History, ActionRunner |
| Agents installed | I2, I3 | agent-box (Tailscale mode, 100.73.66.67:7171), VPS (ssh mode, 127.0.0.1 only); tokens in the Mac Keychain (service `Hotwatch`) |
| Shell + Overview | cdeccf6, eec3d2a, 58358c2, ce8a19c | sidebar with live values and host switcher (Add/Edit/Remove, host-key dialogs), overview card anatomy, 3-column grid, dot-matrix charts, Memory by Type/App, hidden title bar, page header with subtitle, chart window 1m–1h, pause |
| Data | d2c1425, ac4c6e4 | processes for the selected host on every page, History totals/stats, APFS containers counted once, CPU user/system |
| Pages | 4a8bf2c, 2286c9f, 5d2e14b | Energy (ring, power chart), Processes (scopes, footer totals, column order, hot values), Settings (interval, net units, shortcuts, about); hub retunes at runtime |

## Global Constraints

- Module `hotwatch`; Go 1.27; `CGO_ENABLED=0` for every build (app and agent).
- Clean-room (R19): never open `~/Downloads/Pulse.0.1.14-decompiled/` or the Pulse binary; nothing copied from it — the reference is observed only through screenshots of the running app (captured by window id, never full screen) and public OS documentation. Look-alike layouts are rebuilt with our own code and ISC icons.
- Agent never listens on a public or wildcard address (R15). Nothing is installed on agent-box or the VPS without the owner's approval at that moment.
- Process environments are never read; container logs never written to disk; external commands run with argv lists through `mac/exec`, never a shell; every exec has a context timeout.
- Unknown values are `nil` and shown as "—", never 0; a card or stat whose data the host does not report is omitted, never faked.
- UI state that widgets bind to (segmented controls, inputs) lives in state kept between frames, never in per-frame locals (mygo applies bound input to the previous frame's pointer); after a setting changes mid-frame call `c.Invalidate()`.
- Every visual task ends with `go run ./cmd/hwshot <png> <page> <hostIdx>`, the PNG viewed, and the PNG path in the task report.
- Heavy and Linux work runs on agent-box via `scripts/box.sh '<cmd>'`; darwin packages and the app run on the Mac; the Mac repo is the source of truth.
- Commit per task, Conventional Commit subject with requirement IDs, trailer `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- Time budget: none; Must before Should; the cut order below applies if we stop early.

## Review Focus

1. **Icon loads on huge process lists** (3,000 rows, many distinct bundles): the frame never blocks on AppKit; icons arrive asynchronously, cached per bundle path, with an initials tile meanwhile. Tests: P0.2 (`Fake` pending → delivered) and U4 (3,000 rows render < 100 ms with a source that never answers).
2. **Processes and apps without a bundle or an icon** (daemons, helper binaries, a bundle without `.icns`): an initials tile, never an empty cell or a crash. Tests: P0.2 (nil bitmap path) and U4 (row without `AppBundle`).
3. **Apps scan on a slow or huge `/Applications`** (2 GB bundles): sizes are computed lazily off the main thread and cached; the list appears before any size. Test: F1 (`Size` is called only for visible rows; the fake records calls).
4. **`brew`/`mise` missing, slow or failing** (exit 1 with a long log): the page says so with the exit code and last log lines; nothing hangs (3 s list timeout, 10 min task timeout) and nothing shells out. Tests: P0.3 (`exec.System` deadline; `pkgs.Fake{Fail:true}`) and F2 (failure rendering; "not installed" state).
5. **Missing or stale translations** (a key added in English only): the English text shows, never the key or an empty string; every English key has a Vietnamese entry. Tests: P0.4 (completeness + fallback) and S1 (literal-string lint).

---

## Phase 0 — Contracts (sequential)

| Task | Requirements | Files | Proof (test/command) |
|---|---|---|---|
| P0.1 Design tokens from the approved mockup; UI-DESIGN.md "Tokens" updated with the artifact link | R18 | `ui/theme.go`, `ui/components/metric.go`, `ui/theme_test.go`, `UI-DESIGN.md` | `go test ./ui/ -run Theme`; hwshot overview PNG viewed |
| P0.2 `mac/appicon` contract: non-blocking icon `Source` + `Fake`; initials fallback via `ui.Avatar` | R8, R9 | `mac/appicon/appicon.go`, `mac/appicon/fake.go`, `mac/appicon/appicon_test.go` | `go test ./mac/appicon/` |
| P0.3 `mac/exec`, `mac/apps`, `mac/pkgs` contracts: types, `Scanner`/`Manager` interfaces, fakes, command runner with timeouts | R9, R10 | `mac/exec/run.go`, `mac/apps/apps.go`, `mac/apps/fake.go`, `mac/pkgs/pkgs.go`, `mac/pkgs/fake.go`, tests | `go test ./mac/...` |
| P0.4 `ui/i18n` contract: `T(key)`, en/vi tables, missing-key fallback, completeness test | R20 | `ui/i18n/i18n.go`, `ui/i18n/en.go`, `ui/i18n/vi.go`, `ui/i18n/i18n_test.go` | `go test ./ui/i18n/` |

## Phase 1 — Parallel tracks

| Track | Task | Requirements | Files owned | Proof |
|---|---|---|---|---|
| U UI parity (Must) | U1 Detail-page charts become dot matrices with a time axis (CPU, Memory, Disk, Network, GPU), keeping unit + current value | R2–R6, R18 | `ui/components/timechart.go`, `ui/pages/{cpu,memory,disk,network,gpu}.go`, tests | `go test ./ui/...`; 5 PNGs |
| U | U2 Hardware pages to their captured references (after Q3): CPU cores grid, Memory by type/app, Disk volumes, Network interfaces/ports, GPU | R2–R6 | `ui/pages/{cpu,memory,disk,network,gpu}.go` | `go test ./ui/pages/`; PNG per page vs capture |
| U | U3 Re-skin every component to the P0.1 tokens (cards, tiles, badges, tables, sidebar, switcher, dialogs) | R18 | `ui/components/*.go`, `ui/shell.go`, `ui/hostpanel/*.go` | `go test ./ui/...`; overview + processes + energy + dark PNGs |
| U | U4 App icons in Processes and Memory by App through `mac/appicon` (darwin impl) | R8 | `mac/appicon/darwin.go`, `mac/appicon/other.go`, `ui/pages/processes.go`, `ui/pages/overview.go`, `ui/app.go`, `cmd/hotwatch/wire.go` | `go test ./ui/pages/ -run 'Icon|Processes'` incl. 3,000-row budget; Mac PNG |
| U | U5 About This Mac page (Hardware, Software, Displays, Storage) from sysctl/IOKit/snapshot | R18 (identity), demo "every page" | `collect/darwin/sysinfo.go`, `model/host.go` (additive), `ui/pages/about.go`, `ui/shell.go` | `go test ./collect/darwin/ ./ui/pages/ -run About`; PNG |
| F Mac extras (Should) | F1 Apps page: list, sort, search, inspector, uninstall to Trash with confirm | R9 | `mac/apps/darwin.go`, `mac/apps/macho.go`, `ui/pages/apps.go`, `cmd/hotwatch/wire.go` | `go test ./mac/apps/ ./ui/pages/ -run Apps`; PNG |
| F | F2 Packages page: Homebrew formulae/casks + mise, outdated badge, update one/all, remove, prune, live log via ActionRunner | R10 | `mac/pkgs/brew.go`, `mac/pkgs/mise.go`, `ui/pages/packages.go`, `hub/action.go` (kind "package") | `go test ./mac/pkgs/ ./ui/pages/ -run Packages ./hub/`; PNG |
| F | F3 Menu bar item: CPU/RAM of the selected host, click opens the window; Settings "Menu bar shows" | R21 (Could) | `ui/tray.go`, `cmd/hotwatch/main.go`, `ui/pages/settings.go` | `go test ./ui/ -run Tray`; manual on the Mac |
| S Settings (Should) | S1 Vietnamese: `T()` across sidebar, headers, cards, settings, processes; Language selector; persisted | R20 | `ui/**` (strings), `ui/pages/settings.go`, `cmd/hotwatch/main.go` | `go test ./ui/...`; vi PNGs of overview + settings |
| Q Quality (Must) | Q1 Measure fast/light: 60 s averages for the app (3 hosts) and both agents against R17/R25; record in NOTES.md | R17, R25 | `scripts/measure-app.sh`, `scripts/measure-agent.sh`, `NOTES.md` | numbers in NOTES.md under the targets |
| Q | Q2 Bundle `.app`/`.dmg`; stable ad-hoc signing identifier (Keychain prompts stop); DEMO_FLOWS.md; reset script | R22 | `mygo.json`, `assets/icon.png`, `DEMO_FLOWS.md`, `scripts/reset-demo.sh`, `Makefile` | `make dmg`; `.app` launches from Finder; no Keychain prompt after one "Always Allow" |
| Q | Q3 Capture the reference's hardware pages (owner clicks CPU → Memory → GPU → Disk → Network) and write their anatomy into UI-DESIGN.md | R18 | `UI-DESIGN.md` | "Page references" lists all five |
| N Nook (Should, owner-approved 2026-10-15) | N1 `mac/notch`: notch geometry (NSScreen safe area + auxiliary areas via purego) + `Fake`; pure `HoverZones(entry, stay)` | R21 (menu-bar family), R18 | `mac/notch/notch.go`, `mac/notch/darwin.go`, `mac/notch/fake.go`, tests | `go test ./mac/notch/` |
| N | N2 `ui/nook`: idle bar (CPU/Memory halves, warn/offline colours) + island (host chips, 4 tiles with dot sparklines, top processes, pause, open) with state machine idle→expanding→open→closing | R18 | `ui/nook/*.go`, tests | `go test ./ui/nook/`; hwshot PNG of the island |
| N | N3 wire: transparent non-focusable flyouts anchored at the notch, cursor poll 15 Hz, hide when no notch / main window fullscreen, offline alert pop (4 s), Settings "Nook" | R16, R21 | `cmd/hotwatch/nook.go`, `ui/pages/settings.go`, `store` (Settings.Nook) | manual on the Mac; unit tests for the poll state machine |

Track dependencies: U3 needs P0.1; U4 and F1 need P0.2; F1/F2 need P0.3; S1 needs P0.4; U2 needs Q3 (captures) — until then U2 is blocked and U1/U3/U4/U5 proceed. F2 extends the ActionRunner with kind `package` (the `hub.Action` struct is unchanged). F1 reuses kind `uninstall` already declared in `hub.ActionKind`.

## Phase 2 — Final review and ship

| Task | Requirements | Files | Proof |
|---|---|---|---|
| Z1 Whole-branch review on the most capable model; one fix round | all | — | review report clean |
| Z2 PR `feat/v1` → `main` in the private repo (owner opens/merges; never pushed without asking) | R22 | — | PR description with DEMO_FLOWS and measurements |

## Cut order if late

1. F3 menu bar (R21, Could) — N makes it the no-notch fallback only
2. N3/N2/N1 nook (Should)
3. S1 Vietnamese strings (keep the Language selector hidden)
3. F2 Packages (R10)
4. F1 Apps (R9)
5. U5 About This Mac
6. U2 hardware-page anatomy (keep the U1 charts)
7. U4 real icons (keep initials tiles)

Never cut: P0.1, U1, U3, Q1, Q2, Z1.

---

# Task details

Conventions: write the failing test, run it and see it fail for the expected reason, implement the minimum, see it pass, view the PNG for visual tasks, commit with the requirement IDs. Commands marked **(agent-box)** run through `scripts/box.sh`; everything else runs on the Mac.

### P0.1: Design tokens from the approved mockup

**Files:** Modify `ui/theme.go`, `ui/components/metric.go`, `UI-DESIGN.md`; create `ui/theme_test.go`.

**Interfaces:**
- Consumes: the approved Claude Design artifact (link recorded in UI-DESIGN.md "Tokens") and the owner's logo colour. Until the owner provides the hex, the plan uses `#e11d48` (light) / `#fb7185` (dark) as the red-pink stand-in; replace both in the test and the theme on approval.
- Produces: unchanged token names (`Theme(dark).Accent`, `Surface`, `Radius`, …; `components.MetricColor(key)`), new values. Later tasks only read tokens; none hard-codes a colour.

- [ ] **Step 1: Failing test**

```go
package ui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"

	hwui "hotwatch/ui"
	"hotwatch/ui/components"
)

func TestThemeTokensMatchTheApprovedDesign(t *testing.T) {
	light, dark := hwui.Theme(false), hwui.Theme(true)
	if light.Accent != ui.Hex("#e11d48") || dark.Accent != ui.Hex("#fb7185") {
		t.Fatalf("accent %v / %v differs from UI-DESIGN.md Tokens", light.Accent, dark.Accent)
	}
	if light.Radius != 14 {
		t.Fatalf("card radius %v, want 14", light.Radius)
	}
	for _, key := range []string{"cpu", "memory", "gpu", "disk", "network", "energy"} {
		if _, ok := components.MetricColor(key); !ok {
			t.Fatalf("metric %s has no colour", key)
		}
	}
}
```

- [ ] **Step 2:** `go test ./ui/ -run Theme` → FAIL (accent is the v1 teal).
- [ ] **Step 3:** Set the values in `ui/theme.go` (light and dark: `Accent`, `AccentHover`, `AccentPressed`, `Focus`, `Radius 14`); keep `Success/Warning/Danger` semantic; recolour `metricHex` only if the mockup does; write the final table into UI-DESIGN.md "Tokens" with the artifact link.
- [ ] **Step 4:** PASS; `go run ./cmd/hwshot /tmp/p01.png overview 0`, view it.
- [ ] **Step 5: Commit** `feat(R18): design tokens from the approved mockup`

### P0.2: `mac/appicon` contract

**Files:** Create `mac/appicon/appicon.go`, `mac/appicon/fake.go`, `mac/appicon/appicon_test.go`.

**Interfaces:** Produces

```go
package appicon

// Source hands out application icons without blocking the frame: Icon
// returns the cached bitmap, or (nil, false) and schedules a load whose
// completion is delivered through the main-thread hop the implementation
// was built with, after which the caller re-renders and gets it.
type Source interface {
	Icon(bundlePath string) (*ui.Bitmap, bool)
}

// Fake answers from Icons; a path in Pending answers false until Deliver.
type Fake struct {
	Icons   map[string]*ui.Bitmap
	Pending map[string]bool
	Asked   []string
}

func (f *Fake) Icon(p string) (*ui.Bitmap, bool)
func (f *Fake) Deliver(p string) // moves p out of Pending
```

The darwin implementation (`NewDarwin(post func(func()), px int) Source`, `darwin.go`, `//go:build darwin`) lands in U4. Rendering everywhere uses `ui.Avatar(c, name, bitmap)`, which shows initials when the bitmap is nil (Review Focus 2).

- [ ] **Step 1: Failing test**

```go
func TestFakePendingThenDelivered(t *testing.T) {
	f := &Fake{Icons: map[string]*ui.Bitmap{"/Applications/A.app": ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 2, 2)))},
		Pending: map[string]bool{"/Applications/A.app": true}}
	if b, ok := f.Icon("/Applications/A.app"); ok || b != nil {
		t.Fatal("pending icon answered")
	}
	f.Deliver("/Applications/A.app")
	if _, ok := f.Icon("/Applications/A.app"); !ok {
		t.Fatal("delivered icon missing")
	}
	if _, ok := f.Icon("/nope.app"); ok {
		t.Fatal("unknown bundle answered")
	}
	if !slices.Equal(f.Asked, []string{"/Applications/A.app", "/Applications/A.app", "/nope.app"}) {
		t.Fatalf("asked %v", f.Asked)
	}
}
```

- [ ] **Step 2–4:** FAIL → implement → PASS. **Step 5: Commit** `feat(R8,R9): app icon source contract and fake`

### P0.3: `mac/exec`, `mac/apps`, `mac/pkgs` contracts

**Files:** Create `mac/exec/run.go`, `mac/exec/run_test.go`, `mac/apps/apps.go`, `mac/apps/fake.go`, `mac/apps/apps_test.go`, `mac/pkgs/pkgs.go`, `mac/pkgs/fake.go`, `mac/pkgs/pkgs_test.go`.

**Interfaces:** Produces

```go
package exec // mac/exec: every external command in the app goes through here
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)
func System(timeout time.Duration) Runner // os/exec with argv, context timeout, 8 MB output cap
func Streaming(ctx context.Context, name string, args ...string) (lines <-chan string, wait func() error, err error) // live logs: combined stdout+stderr, line by line

package apps
type App struct {
	Name, Bundle, BundleID, Version string
	Size       uint64    // 0: not computed yet
	Arch       string    // "Universal", "Apple silicon", "Intel", ""
	Source     string    // "App Store", "Homebrew", ""
	LastOpened time.Time // zero: never / unknown
}
type Scanner interface {
	Apps(ctx context.Context) ([]App, error)                // fast: no sizes
	Size(ctx context.Context, bundle string) (uint64, error) // slow: walked once, cached by the caller
	Uninstall(ctx context.Context, bundle string) error     // moves to the Trash via Finder (the admin prompt is Finder's)
}
type Fake struct{ List []App; Sizes map[string]uint64; SizeCalls, Removed []string }

package pkgs
type Package struct{ Name, Kind, Version, Newest string; Outdated bool } // Kind "formula"|"cask"|"mise"
type Manager interface {
	List(ctx context.Context) ([]Package, error)
	Available() (brew, mise bool)
	Run(ctx context.Context, verb, name string) (<-chan string, func() error, error) // verb upgrade|upgrade-all|remove|prune
}
type Fake struct{ Pkgs []Package; Log []string; Fail bool; Ran []string }
```

- [ ] **Step 1: Failing tests:** `exec.System(50*time.Millisecond)(ctx, "sleep", "1")` returns a deadline error in under 200 ms; `exec.System` with name `"echo"` and arg `"$HOME"` prints the literal `$HOME` (no shell); `apps.Fake` returns `List` and records `SizeCalls`; `pkgs.Fake{Fail: true}.Run` streams `Log` and `wait()` returns an error containing "exit status 1".
- [ ] **Step 2–4:** FAIL → implement → PASS. **Step 5: Commit** `feat(R9,R10): exec, apps and packages contracts with fakes`

### P0.4: `ui/i18n` contract

**Files:** Create `ui/i18n/i18n.go`, `ui/i18n/en.go`, `ui/i18n/vi.go`, `ui/i18n/i18n_test.go`.

**Interfaces:**

```go
package i18n
type Lang string
const (EN Lang = "en"; VI Lang = "vi")
func Set(l Lang)          // main thread; unknown → EN
func Current() Lang
func T(key string) string // vi, else en, else the key itself
var en = map[string]string{"nav.overview": "Overview", "nav.processes": "Processes", "nav.cpu": "CPU", /* every visible label, ~90 keys */}
var vi = map[string]string{"nav.overview": "Tổng quan", "nav.processes": "Tiến trình", /* … */}
```

- [ ] **Step 1: Failing tests**

```go
func TestEveryEnglishKeyHasVietnamese(t *testing.T) {
	for k := range en {
		if _, ok := vi[k]; !ok {
			t.Errorf("missing vi for %q", k)
		}
	}
}

func TestFallbacks(t *testing.T) {
	Set(VI)
	defer Set(EN)
	if T("nav.overview") != "Tổng quan" || T("no.such.key") != "no.such.key" {
		t.Fatal("fallback")
	}
	delete(vi, "nav.cpu") // a missing entry
	defer func() { vi["nav.cpu"] = "CPU" }()
	if T("nav.cpu") != "CPU" {
		t.Fatal("en fallback")
	}
}
```

- [ ] **Step 2–4:** FAIL → implement (tables start with the sidebar, header subtitles, overview card labels, settings labels, processes toolbar/footer; S1 wires them) → PASS. **Step 5: Commit** `feat(R20): i18n tables and T()`

---

## Track U — UI parity

### U1: Detail-page charts as dot matrices with a time axis

**Files:** Modify `ui/components/timechart.go` (replace `TimeChart` with `DotTimeChart`), `ui/pages/{cpu,memory,disk,network,gpu}.go`; tests in `ui/components/timechart_test.go`, `ui/pages/pages_test.go`.

**Interfaces:**
- Consumes `components.DotChart(c, pts, from, to, ceiling, tint, height)` and `components.ChartWindow()` (exist).
- Produces `components.DotTimeChart(c *ui.Context, title, unit string, series []Series, now time.Time, tint ui.Color)` with `type Series struct{ Name string; Pts []hub.Point; Format func(float64) string }`: card header with the title and unit, a legend "Name · current value" per series (R18), the dot matrix of the first series (a second series draws in a lighter tint on the same matrix — Read beside Write, In beside Out), and the time axis from `xTicksFor`.

- [ ] **Step 1: Failing test** (`ui/components`): render `DotTimeChart("Network", "B/s", []Series{{"In", …}, {"Out", …}}, now, tint)` in a `Tester`; assert texts contain "Network", "B/s", "In · 27 KB/s", "Out · 15 KB/s" and the axis labels ending with "now". Also assert in `ui/pages/pages_test.go` that `TimeChart` no longer exists by compiling (remove the old function in Step 3; the build fails if a page still calls it).
- [ ] **Step 2:** FAIL. **Step 3:** implement; switch the five pages (CPU total %, Memory used %, Disk read+write, Network in+out, GPU %) passing the page's `MetricColor`. **Step 4:** PASS; `hwshot` the five pages and view them. **Step 5: Commit** `feat(R2-R6,R18): dot-matrix charts on the detail pages`

### U2: Hardware pages to their captured references

Blocked on Q3. **Files:** `ui/pages/{cpu,memory,disk,network,gpu}.go`, tests. For each page, write its anatomy into UI-DESIGN.md "Page references" from the capture, then: header stats row (reuse Energy's `statCell`), the U1 chart, then the page body — CPU: cores as small tiles (one percent each, performance/efficiency groups when `HostInfo.PerfCores > 0`); Memory: Memory by Type donut + Memory by App (exists on overview, moves here as the primary); Disk: one card per volume with capacity bar + read/write; Network: interfaces table (name, addresses, in/out) + listening ports table (port, proto, process); GPU: utilisation, VRAM bar, temperature.

- [ ] **Step 1 (per page): Failing test** asserting the stat labels and table headers of that page's capture. **Step 2–4.** **Step 5:** PNG beside its capture; commit per page: `feat(R2): cpu page to the reference anatomy`, `feat(R3): …`, `feat(R4): …`, `feat(R5): …`, `feat(R6): …`.

### U3: Re-skin to the approved tokens

**Files:** Modify `ui/components/{metric,badge,bars,donut,sparkline,banner,empty}.go`, `ui/shell.go`, `ui/hostpanel/*.go`; test `ui/components/skin_test.go`.

- [ ] **Step 1: Failing test:** render `Badge`, `IconTile` and a `Surface` in a `Tester`; sample the pixel at each element's rect centre (`tt.Image().At(x, y)`) and assert it equals the theme token the element must use (`Theme(false).Accent.Alpha(0.14)` over `Surface` for the tile); assert the WCAG contrast of `TextMuted` on `Surface` and of white on `Accent` ≥ 4.5 using a `contrast(a, b ui.Color) float64` helper in the test.
- [ ] **Step 2–4:** apply the mockup's spacing, radii (14 cards / 8 tiles), tile tints, selected-row style, sidebar, switcher and dialogs. **Step 5:** PNGs: overview, processes, energy, settings, dark overview (add a `--dark` flag to hwshot: `mui.RenderDark` or `tt.SetDark(true)` equivalent). Commit `feat(R18): re-skin to the approved design`

### U4: App icons in Processes and Memory by App

**Files:** Create `mac/appicon/darwin.go` (`//go:build darwin`), `mac/appicon/other.go` (`!darwin`: `NewDarwin` returns a source that always answers false); modify `ui/pages/processes.go`, `ui/pages/overview.go`, `ui/pages/icons.go` (new: `SetIconSource(appicon.Source)`), `ui/app.go` (`SetIconSource` forwards), `cmd/hotwatch/wire.go` (installs `appicon.NewDarwin(c.post, 32)`), tests.

**Interfaces:** `pages.SetIconSource(src appicon.Source)` — package-level, main thread, nil = initials only (pages are functions, so no struct to carry it).

- [ ] **Step 1: Failing tests:** (a) `processes_test`: with a `Fake` serving `/Applications/Google Chrome.app`, the Name cell contains an `Avatar` with that bitmap (Tester: `tt.Find("Google Chrome")` has an image child 20×20); with `Pending`, the cell shows the initials "GC" and after `Deliver` + `tt.Frame()` the bitmap; a row without `AppBundle` shows initials; (b) the existing 3,000-row budget test runs with a `Fake` that never answers and stays < 100 ms.
- [ ] **Step 2–4:** implement. Darwin: `objc_msgSend` via purego (`[NSWorkspace sharedWorkspace]`, `iconForFile:`, `CGImageForProposedRect:context:hints:`, `CGBitmapContextCreate` RGBA 32 px, `CGContextDrawImage`); loads on a worker goroutine behind a semaphore of 2, delivered with `post`, cache keyed by bundle path (LRU 512). Mac test: `Icon("/System/Applications/Calculator.app")` becomes available within 2 s. **Step 5:** Mac PNG of Processes with real icons. Commit `feat(R8): application icons in the process list`

### U5: About This Mac page

**Files:** Create `collect/darwin/sysinfo.go` (`hw.model`, `machdep.cpu.brand_string`, `hw.perflevel0.physicalcpu`/`hw.perflevel1.physicalcpu`, `kern.osproductversion`, `kern.osversion`, serial via IOKit `IOPlatformExpertDevice` → `IOPlatformSerialNumber`, main display via `CGMainDisplayID` + `CGDisplayPixelsWide/High`), `ui/pages/about.go`, `ui/pages/about_test.go`; modify `model/host.go` (additive, all `omitempty`: `Model, Chip, OSVersion, Build, Serial string; PerfCores, EffCores, GPUCores int; DisplayW, DisplayH int`), `source/local` (fills them once at start), `ui/shell.go` (sidebar row "About This Mac" for the local host only).

- [ ] **Step 1: Failing tests:** `collect/darwin`: `Sysinfo()` returns a non-empty `Model` and a `Chip` containing "Apple"; `about_test`: renders Hardware/Software/Storage cards with the labels Model, Model identifier, Chip, "CPU cores" → "10 (4 performance, 6 efficiency)", Memory, System, Build, Kernel, Computer name, Uptime, Processes, and storage "13.4 GB free of 494 GB" from `DistinctVolumes`.
- [ ] **Step 2–4:** implement (Linux hosts show no About row; the agent is unchanged). **Step 5:** PNG. Commit `feat(R18): about this mac page`

## Track F — Mac extras

### F1: Apps page

**Files:** Create `mac/apps/darwin.go`, `mac/apps/macho.go`, `ui/pages/apps.go`, `ui/pages/apps_test.go`; modify `cmd/hotwatch/wire.go` (installs `apps.New(exec.System(3*time.Second))`), `ui/app.go` (`SetApps(apps.Scanner)`), `hub/action.go` (kind `uninstall` executes `Scanner.Uninstall`; confirm/progress UI shared with kill).

Scan: `/Applications`, `/Applications/Utilities`, `~/Applications`, `/System/Applications` (listed, not uninstallable); `Info.plist` with `encoding/xml` for XML plists, `plutil -convert xml1 -o - <path>` (argv) for binary ones. Arch from the executable's Mach-O magic (`0xcafebabe` fat → "Universal"; `0xfeedfacf` with cputype arm64 → "Apple silicon"; x86_64 → "Intel"). Source: `Contents/_MASReceipt/receipt` → "App Store"; names in `brew list --cask -1` → "Homebrew". Last opened: `mdls -raw -name kMDItemLastUsedDate` once per bundle, batched in one call. Size: `filepath.WalkDir`, cached per bundle for the session, requested by the page for visible rows only, computed on a worker and delivered with `post`.

- [ ] **Step 1: Failing tests:** page renders the fake's apps with columns Name, Version, Size, Kind, Last Opened, Source; search narrows; selecting a row opens the inspector with Version/Size/Kind/Path and an Uninstall button that asks "Move “X” to the Trash?"; confirming records `Removed`; `SizeCalls` only contains bundles of rows that were visible. Darwin test: `Apps()` on this Mac includes "Safari" with Arch "Universal" or "Apple silicon".
- [ ] **Step 2–4:** implement. **Step 5:** PNG. Commit `feat(R9): apps page with uninstall`

### F2: Packages page

**Files:** Create `mac/pkgs/brew.go`, `mac/pkgs/mise.go`, `mac/pkgs/testdata/{brew-info.json,brew-outdated.json,mise-ls.json,mise-outdated.json}`, `ui/pages/packages.go`, tests; modify `hub/action.go` (kind `package`: `Verb`, `Target`, streamed `Log` lines, exit code in `Err`), `cmd/hotwatch/wire.go`.

brew: `brew info --json=v2 --installed` (names, versions) + `brew outdated --json=v2` (newest versions; casks under `casks`). mise: `mise ls --json` + `mise outdated --json`. Tasks via `exec.Streaming`: `brew upgrade <name>`, `brew upgrade`, `brew uninstall <name>`, `brew cleanup --prune=all`, `mise upgrade <tool>`, `mise prune --yes`; list timeout 3 s, task timeout 10 min; the page shows the running task's last 200 lines; on failure the exit code with the last 20 lines.

- [ ] **Step 1: Failing tests:** parsers from the fixtures produce the expected packages with `Outdated` and `Newest` set (fixture: 3 outdated); page: tabs Homebrew/mise, badge "3 outdated", "Update" on a row starts a `package` action whose log lines appear and whose success clears the row's flag (fake manager); `Fail: true` shows "exit status 1" and the last lines; `Available()` false → "Homebrew is not installed" instead of an empty table.
- [ ] **Step 2–4:** implement. **Step 5:** PNG. Commit `feat(R10): packages page with live task log`

### F3: Menu bar item (Could)

**Files:** Create `ui/tray.go`, `ui/tray_test.go`; modify `cmd/hotwatch/main.go`, `ui/pages/settings.go` ("Menu bar shows": CPU · Memory · Both · Nothing → `Settings.MenuBar` string, persisted).

- [ ] **Step 1: Failing test:** `TrayTitle(view, "both")` → "CPU 32% · RAM 79%"; `"cpu"` → "CPU 32%"; `"off"` → ""; a nil snapshot → "—". **Step 2–4:** implement with mygo's tray API (`go doc github.com/egoist/mygo | grep -i tray` first), title updated on every publish on the main thread, click brings the window forward. **Step 5:** manual check on the Mac. Commit `feat(R21): menu bar item`

## Track S — Settings

### S1: Vietnamese

**Files:** Modify `ui/shell.go`, `ui/pages/*.go`, `ui/hostpanel/*.go` (literals → `i18n.T`), `ui/pages/settings.go` (Language: Same as System · English · Tiếng Việt → `Settings.Language` "", "en", "vi"), `cmd/hotwatch/main.go` (`i18n.Set` before the window; "system" from `defaults read -g AppleLanguages` through `exec.System`, first entry prefix "vi" → VI); test `ui/i18n_wire_test.go`, `ui/strings_lint_test.go`.

- [ ] **Step 1: Failing tests:** with `Language: "vi"` the sidebar shows "Tổng quan" and "Tiến trình", the overview card keeps "CPU", Settings shows "Cập nhật mỗi"; clicking "English" restores the texts on the next frame. Lint: a test walks `ui/**/*.go` and fails on `ui.Text(c, "<ASCII literal of 4+ letters>"` outside an allowlist (units, symbols, "CPU", "GPU", "PID").
- [ ] **Step 2–4:** implement. **Step 5:** PNGs en + vi. Commit `feat(R20): vietnamese interface`

## Track Q — Quality and ship

### Q1: Measure fast/light

**Files:** Create `scripts/measure-app.sh` (`top -l 31 -s 2 -pid <app>` averaged; RSS from `ps -o rss`; 3 hosts configured), `scripts/measure-agent.sh` (run over ssh on each host: `/proc/<pid>/stat` utime+stime deltas and `VmRSS` sampled 60 s, no extra tools installed); modify `NOTES.md`.

- [ ] **Step 1:** run both; record app CPU %, RSS, `ui.first_data` and `ui.switch` timings from the log, agent CPU % and RSS on agent-box and the VPS, and the reference app side by side (same 60 s). **Step 2:** compare with R17 (app < 2 % / 150 MB; agent < 1 % / 30 MB) and R25 (< 1 s first data, < 100 ms switch); a missed target becomes its own task (profile first: 60 s CPU profile behind a hidden `-cpuprofile` flag). **Step 3: Commit** `docs(R17,R25): measurements`

### Q2: Bundle and demo

**Files:** Create `mygo.json` (bundle id `dev.cicca.hotwatch`, name Hotwatch, icon `assets/icon.png` drawn for this project in the P0.1 palette), `DEMO_FLOWS.md`, `scripts/reset-demo.sh` (removes the config and the Keychain items after asking `[y/N]`); modify `Makefile` (`dmg` target; `codesign --force --sign - --identifier dev.cicca.hotwatch` on the bundle so Keychain's "Always Allow" survives rebuilds).

- [ ] **Step 1:** `make dmg` produces `dist/Hotwatch.dmg`; the `.app` launches from Finder; after one "Always Allow" a rebuilt `.app` opens without a Keychain prompt. DEMO_FLOWS.md sections: Mac overview live; add the VPS; Docker on the VPS (list, restart `jobhunter-bot`, logs); Apps + Packages; every page — each step "target text → label (R-ID)". **Step 2: Commit** `build(R22): app bundle, demo flows, reset script`

### Q3: Capture the reference's hardware pages

Owner clicks CPU → Memory → GPU → Disk → Network in the reference app while the window-id capture loop runs (`screencapture -x -o -l<id>` every 2 s; the lister lives in the session scratchpad — never full-screen). Keep one PNG per page in the scratchpad (never in the repo); write each page's anatomy into UI-DESIGN.md "Page references". Commit `docs(R18): hardware page references`.

---

## Self-review notes

- Spec coverage: R9 (F1), R10 (F2), R20 (P0.4, S1), R21 (F3), R17/R25 (Q1), R22 (Q2), R18 (P0.1, U1, U3; U5 for the demo's "every page"), R8 icons (U4), R2–R6 pages (U1, U2). R11–R16 contracts are untouched.
- Placeholders: the two accent hex values in P0.1 are the one deliberate input from the owner (logo colour); the stand-in and where it comes from are named.
- Types: `appicon.Source`, `apps.Scanner`, `pkgs.Manager`, `exec.Runner`, `components.Series` are defined once and used by name in U4, F1, F2, U1; `hub.Action` gains behaviour for kinds `uninstall` and `package` without changing its fields.
- Review Focus: each line names its tests (P0.2/U4, P0.2/U4, F1, P0.3/F2, P0.4/S1).
