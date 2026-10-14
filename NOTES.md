# Notes (my own reading of the brief and data)

## Data observations
- No data/ pack. Inputs are the owner's messages and CLAUDE.md Project context.
- mygo (github.com/egoist/mygo, MIT, Go 1.27+, pure Go/no cgo) offers native GPU-drawn UI (`ui` package) or webview pages; builds `.app`/`.dmg`, `.deb` and a Linux install script; desktop APIs include tray, menus, dialogs, notifications and global shortcuts.
- VPS (read-only check 2026-10-14): Ubuntu 24.04.4 x86_64, 4 vCPU, 7.9 GB RAM, Docker 29.4.3 with one container `jobhunter-bot`, Tailscale IP 100.87.226.98, systemd 255.
- agent-box: Ubuntu, 16 cores, 62 GB RAM, RTX 3080, Docker, Tailscale 100.73.66.67.
- Clean-room boundary: Pulse's decompiled output lives in `~/Downloads/Pulse.0.1.14-decompiled/`. Do not open it while building hotwatch; implement from OS documentation (IOKit, sysctl, libproc, /proc, Docker Engine API).

## Questions / assumptions
See REQUIREMENTS.md "Open questions / assumptions" (Q1–Q7).
