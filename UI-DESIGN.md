# UI design — Overview and shell

Target: the polish of a first-class macOS monitor, with hotwatch's own
identity (rose accent, warm neutrals, Lucide icons). Written before the
second visual pass so the pass is one coherent change, not a series of
tweaks. Numbers are DIPs.

## Tokens

Approved by the owner on 2026-10-15 from the Claude Design canvas
<https://claude.ai/artifact/Auh9cnk3QATuw6PgGgkjSU> (22 artboards: every
page, light + dark, Linux hosts, Docker, failure states, nook). The accent
is a stand-in for the owner's portfolio logo colour until the exact hex is
given; `ui/theme_test.go` pins these values.

| Token | Light | Dark | Use |
|---|---|---|---|
| `bg` | #f6f3f4 | #121014 | window |
| `surface` | #ffffff | #1b181c | cards, sidebar |
| `surfaceHover` / `border` | #efe9eb / #ece6e8 | #262127 / #2a2429 | segmented tracks, neutral tiles / hairlines |
| `text` / `muted` | #1c1619 / #6f6469 | #ede7e9 / #9a8f94 | copy / labels |
| `accent` | #e11d48 | #fb7185 | selected row, primary buttons, links, update arrows, focus |
| `success` / `warning` / `danger` | #15803d / #b45309 / #b91c1c | #4ade80 / #fbbf24 / #f87171 | badges, hot values, offline |
| metric colours | cpu #6366f1 · memory #10b981 · gpu #f97316 · disk #0ea5e9 · network #14b8a6 · energy #f59e0b | same | tiles, titles, charts — none red, so the accent stays the only red |

Tints are the metric colour at an alpha: `tile` 0.14, `chartBg` 0.10,
`dotOff` 0.18, `dotOn` 1.0. Card radius 14, chart-backdrop radius 10, tile
radius 8 (cards) / 6 (sidebar). Grid unit 8. Numbers use `tnum`.

Type scale: title 15/600, sub-label 13/400 muted, headline 40/700 with unit
16/500 muted on the same baseline, stat label 12/400 muted over stat value
15/600, body 13.

## Window

No separate title bar: the window is `Row[sidebar 200 | content]`; macOS
traffic lights sit over the sidebar's top padding (24). No brand text.
Content padding 20, gap 16. Content scrolls; the sidebar is fixed.

Top of content: page title 22/700 on the left; on the right a segmented
chart window `1m · 5m · 15m · 1h` (drives `components.ChartWindow`) and a
pause toggle. "Updated Xs ago" moves into the host switcher subtitle.

## Sidebar (200)

Rows 30 high, 6 vertical gap, 8 horizontal padding, radius 8. Icon tile 22,
solid metric colour with white glyph; neutral rows use `surfaceHover` tile
with muted glyph. Label 13/500; live value 12 muted, right-aligned.
Selected row: accent background, white label/value, tile white at 0.22.
Section labels ("Hardware", "System") 11/600 muted, 16 top padding.
Processes row shows the process count. Bottom: host switcher as now
(status dot · name 13/600 · subtitle 11 muted: "Apple M5 · Up 28d 4h" when
online, else the connection state), with the host list and "Add host…"
above it when open.

## Overview grid

Three equal columns at every width ≥ 900 (two below); cards stretch to the
row's height. Order: CPU, Memory, GPU (if any), Disk, Network, Battery (if
any), then Memory by Type, Memory by App, Energy by App (if data). Cards
flow row by row: a host without a GPU or a battery (every remote server)
would otherwise show holes, so fixed slots were dropped.

### Metric card anatomy

```
[tile] Title                              ›
Sub-label
40 Headline unit
label      label      label
value      value      value
[ chart backdrop: dot matrix ]
```

| Card | Sub-label | Headline | Stats |
|---|---|---|---|
| CPU | Now | total % | User % · System % · Temperature °C (— if unknown) |
| Memory | In use of {total} | used GB | App · Wired · Compressed (Linux: Used · Cached · Buffers) + pressure badge Normal/Warning/Critical |
| GPU | {name} | util % | Memory · Average (window) · Peak (window) |
| Disk | Free of {total} | free GB | Reading · Writing · Written (session) |
| Network | Downloading | rx rate | Uploading · Received · Sent (session) |
| Battery | On battery / Charging / On AC | charge % | Remaining · Power draw · Health |

Header tile: `tile` tint background with the glyph in the metric colour
(not white): cards read lighter than the sidebar. Title in the metric
colour. Chevron muted; the whole header is a button to the page.

### Dot-matrix chart

Inside a backdrop (`chartBg` tint, radius 10, padding 10, height 72). The
matrix is 8 rows; columns fill the width at a 6 pitch (≈60 columns at the
card's width); dot diameter 3.4. Each column is one bucket of the chart
window (`ChartWindow / columns`), averaged; empty buckets stay all-off. A
column lights `round(v/top*8)` dots from the bottom in `dotOn`; the rest
`dotOff`. `top` is 100 for percentages, else the window's max with 8%
headroom. For CPU the topmost lit dot of each column is drawn in a second
series colour when per-core data exists (user vs system); otherwise one
colour. No lines, no gradients.

### Analytics row

- **Memory by Type**: donut 110 (ring 0.62), centre "{used%}" 22/700 over
  "in use" 11 muted; legend to the right: swatch 10, label, value — App,
  Wired, Compressed, Cached, Free (Free muted).
- **Memory by App**: a tile mosaic sized by RSS: the largest app gets a
  2×2 tile, the next two 1×2, then 1×1, within a 4×2 cell grid; each tile
  shows the app icon (NSWorkspace icon via bundle path; fallback initial
  letter in a tinted square), name 13/600 and size 12 muted; small tiles
  show only the icon. Header right: "{sum} in all".
- **Energy by App**: rows: icon · name · mW right-aligned, a thin bar
  (accent) below scaled to the top app. Header right: "{sum} in all".
  Needs per-process power from the collector (macOS: `powermetrics` is
  root-only; use `proc_pid_rusage` energy counters via libproc). Until the
  collector reports it the card is omitted, never faked.

## Data the Overview needs

- **Processes for the selected host on every page**, not only on
  Processes: the hub's "selected host" tier fetches processes at the normal
  interval so Memory by App and Energy by App populate. Non-selected hosts
  keep the light tier.
- **Session counters**: bytes written / received / sent since the app
  started, kept in `hub.History` as monotonic series.
- **Peak / average** over the chart window come from `hub.History`.

## Bugs found by the side-by-side (fix in the same pass)

- GPU utilisation on Apple silicon read ~5% while the reference showed 57%
  at about the same moment. Checked: we read `Device Utilization %`, and
  `ioreg -r -c IOAccelerator -d 1` shows it equal to `Renderer Utilization %`
  (18 = 18 while checking), so the key is right and the gap is sampling:
  GPU load is spiky, and one 2 s instantaneous read differs from a smoothed
  figure. Keep the key; the card shows the window average and peak from
  `hub.History` beside the instantaneous value, as the anatomy table says.
- Disk total shows 937 GB on a 494 GB Mac: APFS volumes sharing one
  container are summed twice. Group volumes by container (store) and count
  each container once; show the boot container on the card, all on the
  Disk page.

## Page references (captured from the running reference app, 2026-10-15)

What each page shows, as a checklist for the per-page passes. Figures in
quotes are the reference's labels; hotwatch reimplements the content with
its own code and only where it has the data (gaps listed at the end).

- **Page header**: title 22/700 with a one-line subtitle under it
  ("Battery and power", "623 processes · 5,027 threads", "31 apps ·
  21.8 GB · 1 update"); top-right: the chart window segmented control
  `1m · 5m · 15m · 1h`, a pause toggle and a settings (gear) button.
- **Energy**: card *Battery* — donut with "82%" and "On Battery" in the
  centre, a "✓ Normal" badge top-right, six stats in a 3×2 grid each with a
  value and a muted sub-line: Remaining 4h 14m · Battery Flow 18.4 W
  "discharging" · Health 96% "of design capacity" · Cycles 92 · Capacity
  6,012 mAh "design 6,249" · Adapter None. Card *Power* — sub-label "The
  whole computer draws", headline 18.4 W, stats Processes · Average ·
  Peak · Thermal "Nominal / Cool", then a full-width dot chart with a time
  axis (−1m · −45s · −30s · −15s · now). Row: *Energy by App* (app icon,
  name, W/mW right-aligned, thin bar; header right "3.00 W in all") and
  *Temperatures* (Processor "hottest 57 °C", Storage, Battery: bar + °C).
- **Processes**: search field; filter segmented All · Apps · My Processes
  · System; toolbar (tree, end process, inspector). Columns: Name (app
  icon, disclosure for groups, child-count badge), % CPU (sorted, high
  values in orange), Memory (orange when high), Power, Threads, CPU Time,
  PID, User. Footer: "364 of 623 processes · 181 of other users estimated"
  left; totals right "CPU 30% · Memory 20.6 GB · Power 18.4 W".
- **Apps**: tabs Apps · Homebrew · mise; columns Name (icon, running dot),
  Version (update shown as "1.49.2 → 1.53.0"), Size, Kind (Universal /
  Apple silicon / Intel), Last Opened, Source (App Store / Homebrew / –).
  Inspector on the right: icon, name, bundle id, description, a primary
  "Update to 1.53.0", Open · Uninstall · reveal, a Details table (Version,
  Newest, Size, Kind, Added, Last opened, Updates, Cask, Installed, Tap,
  Homepage) and Path. Footer: "Checked Yesterday · 1 update · Update All".
- **About This Mac**: hero card (device picture, model name 32/700,
  "Apple M5 · 24.0 GB", pills for the OS version and uptime), then 2×2
  cards: Hardware (Model, Model identifier, Chip, CPU cores "10 (4
  Performance, 6 Efficiency)", GPU cores, Memory, Architecture, Serial),
  Software (System, Build, Kernel, Computer name, Started, Uptime,
  Processes, Go), Displays (resolution @2x), Storage ("13.5 GB free of
  494 GB").
- **Settings** (popover from the gear): Update every 0.5 s · 1 s · 2 s ·
  5 s; Network speeds in Bytes/s · Bits/s; Menu bar shows (dropdown);
  Language (Same as System); a shortcuts hint; Check for Updates…; Send
  Feedback….
- **Sidebar**: as built, plus the process count on Processes; the bottom
  block reads "MacBook Pro / Apple M5 · Up 28d 5h".

Data hotwatch does not collect yet (never faked; the card or stat is
omitted until a collector reports it): battery cycles, design/actual
capacity, health and adapter (IOKit AppleSmartBattery); per-process power
and the whole-computer draw on battery-less hosts; CPU, storage and
battery temperatures (SMC / IOHID); CPU performance/efficiency core split,
GPU core count, model identifier and serial (sysctl, IOKit); app icons;
Homebrew and mise inventories (F2); the hardware pages' references (CPU,
Memory, GPU, Disk, Network) are still to be captured.

## Out of scope for this pass

Per-page redesigns follow the same tokens, the card anatomy above and the
page references; this pass was the shell, the Overview and the data fixes.
