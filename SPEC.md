# SPEC — change in flight

The current change is the **JEV-driven strategy runtime prototype** (core,
terminal operator console, web console). Its spec and wire contract live in:

- `docs/jev/SPEC.md` — motivation, architecture, plugin contract, runtime,
  governor, reference strategies, terminal and web surfaces, verification bar.
- `docs/jev/PROTOCOL.md` — the JSON/HTTP/WS contract shared by
  `backend/`, `tui/`, and `hypertrade`.
- `docs/jev/RESEARCH.md` — Jev API facts, question design guidance, venue survey.

The previous change (harness-backed reasoning; `pi`/`claude`/`codex` providers,
`doctor`, `auth`) is merged and documented in `backend/README.md`; its spec is
in git history.
