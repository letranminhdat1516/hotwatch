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
