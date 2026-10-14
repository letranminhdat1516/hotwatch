# Project context (fill in after /dev-init; the /dev-* commands read this)
- Goal / brief: <path to brief, ticket or RFP>
- Time budget: <e.g. 150 min total; build phase ends at minute 125> (or "none")
- Must-show in the demo: <the flows a reviewer must see>
- Quality metric (if any): <e.g. agreement vs reference scores, latency, accuracy>
- Ship target: <e.g. private GitHub repo; PR to main; deploy>
- Reviewers / share with: <names or emails>  (never invite or send without asking me)
- Extra deliverables: <e.g. recording link, report>

# Rules for agents in this repo
- Follow PLAN.md. Do not change contracts (types, state machine, schema, LLM interface) without asking.
- TDD: write the failing test first, then the code. Run tests and show output before claiming done.
- One task = one commit; message references requirement IDs (e.g. "feat(R3,R5): ...").
- Never commit .env, databases, node_modules, build output.
- LLM output is untrusted: always validate before use. Never fail silently.
- Keep it small. Do not add features that are not in REQUIREMENTS.md.
- Write specs into REQUIREMENTS.md / ARCHITECTURE.md / PLAN.md (not docs/superpowers/specs/).

# UI (the demo is judged on screen — keep it readable)
- Every web page shares one base layout with one small inline <style> block. No CSS framework, no build step, no JS framework.
- Base look: system-ui font, line-height 1.5, light grey page (#f6f6f4), dark navy top nav (#1f3a5f) with white links and Sign out on the right, content max-width ~72rem centred with padding.
- Tables: full width, white, 1px light borders, padded cells, shaded header, zebra rows. Forms: inputs and buttons use the page font, padded, rounded 4px.
- Show state at a glance: status/flag badges (coloured pill per state, e.g. green = done, amber = needs a person, red = not graded / error, grey = superseded/late), and a clear banner when running on a fake/mock LLM.
- Browsers get a styled HTML page for 4xx/5xx errors (keep the nav, link back); API clients keep JSON errors. Never show a raw stack trace or JSON to a browser.
- Labels in plain words a recruiter understands; each page has an h1 saying what it is, and an empty state ("No answers need review").
- Keep a DEMO_FLOWS.md in the root (one section per flow: URL/command, then numbered steps "target text → label (R-ID)") and a reset command for clean demo data, so the /demo-ui-flows skill can walk through it.
