# Notes (my own reading of the brief and data)

## Data observations
- No data/ pack. Inputs are the owner's messages and CLAUDE.md Project context.
- mygo (github.com/egoist/mygo, MIT, Go 1.27+, pure Go/no cgo) offers native GPU-drawn UI (`ui` package) or webview pages; builds `.app`/`.dmg`, `.deb` and a Linux install script; desktop APIs include tray, menus, dialogs, notifications and global shortcuts.
- VPS (read-only check 2026-10-14): Ubuntu 24.04.4 x86_64, 4 vCPU, 7.9 GB RAM, Docker 29.4.3 with one container `jobhunter-bot`, Tailscale IP 100.87.226.98, systemd 255.
- agent-box: Ubuntu, 16 cores, 62 GB RAM, RTX 3080, Docker, Tailscale 100.73.66.67.
- Clean-room boundary: Pulse's decompiled output lives in `~/Downloads/Pulse.0.1.14-decompiled/`. Do not open it while building hotwatch; implement from OS documentation (IOKit, sysctl, libproc, /proc, Docker Engine API).

## Questions / assumptions
See REQUIREMENTS.md "Open questions / assumptions" (Q1–Q7).

## Decisions

- T0.9 collector spike (Mac, M5, non-root uid 501, CGO_ENABLED=0, gopsutil v4.26.9, 667 processes by `ps -A | wc -l`):
- Build and TestBasicsNoCgo (cpu.Percent, mem.VirtualMemory): OK, yes.
- Basic scan (Name, CPUPercent, MemoryInfo per process): 18.5 ms/scan (18,504,467 ns/op, 5x).
- Full scan (+ CreateTime, Username, Cmdline, NumThreads): 37.9 ms/scan (37,885,508 ns/op, 5x).
- Non-root errors per scan (about, 1025/5): Cmdline error ~205 and Name error ~66 (other users' processes); MemoryInfo returns no error but RSS==0 for ~205; CPUPercent, CreateTime, Username, NumThreads: 0 errors.
- Decision: both scans are <= 50 ms, so B1/B2 use gopsutil for system CPU/mem and the process scan; error-prone fields (Cmdline, Name, RSS) must tolerate failures and show unknown ("—"), not 0. Own libproc via purego is not needed for now; revisit only if the scan exceeds budget on 3,000+ process hosts (not measured here; Linux not tested).
