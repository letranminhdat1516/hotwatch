# Demo flows (used by the `/demo-ui-flows` skill)

## Setup
- Base URL: `http://127.0.0.1:8000`
- Start (outside the sandbox, from the repo root): `<serve command>`
- Reset before a full run (server stopped): `<e.g. cp demo-clean.db demo.db>`
- Login: `<user>` / `<password>` (local demo account only)

## Flow 1 — Traceability (terminal)
Command: `<show README traceability table>`
1. Header row → "N requirements → code → test → evidence"

## Flow 2 — Full data run + agreement (terminal)
Commands: `<grade all>` then `<agreement report>`
1. `graded: X  not graded: Y` → "failures shown, never a low score"
2. Disagreements block → "why the system and the reference differ"

## Flow 3 — Needs-a-person queue
URL: `<queue page>`
1. `<answer id>` row → "<flag>: <why> (R..)"

## Flow 4 — Manage hosts and read the connection log (app)
Command: `make run` (or open Hotwatch.app), then sidebar → Hosts (or host switcher → Manage hosts…)
1. Group headings "Local", "Servers" → "hosts by group, each with how it is reached (R27, R24)"
2. "Edit…" on VPS → change Address → "Save" → "edit a host without a right-click menu (R27, R11)"
3. "Move" on VPS → pick a group → "move between groups (R24)"
4. "Remove…" on a test host → "Remove" in the dialog → "remove asks first (R27, R11)"
5. "Connection log · VPS" → "time, state and reason of each connection change (R27)"
6. Stop the VPS agent → an "Offline" line with the error appears → Retry → "Connecting", then "Online" (R27)
