# Documentation — Hyperion

Welcome to Hyperion documentation. Here's how to navigate.

---

## Quick Navigation

### For Users (Traders)

1. **[QUICKSTART.md](QUICKSTART.md)** — get running in 5 minutes
2. **[ARCHITECTURE.md](ARCHITECTURE.md)** — understand how it works
3. **[API.md](API.md)** — HTTP + MCP interface reference

### For Developers

1. **[ARCHITECTURE.md](ARCHITECTURE.md)** — system design, components, data flow
2. **[DESIGN.md](DESIGN.md)** — product decisions and tradeoffs
3. **Backend README** — build, test, deploy instructions

---

## Documentation by Topic

### Getting Started

- **[QUICKSTART.md](QUICKSTART.md)** — Setup, running backend/TUI, first mandate, keyboard shortcuts

### How It Works

- **[ARCHITECTURE.md](ARCHITECTURE.md)** — System diagram, components (daemon, TUI, dashboard, MCP), data flow, risk gates, signing, journal
- **[DESIGN.md](DESIGN.md)** — Why we made specific choices (mandate-driven UI, compiled gates, event bus, append-only journal, etc.)

### Building & Operating

- **[API.md](API.md)** — HTTP API (markets, positions, orders, journal, control), WebSocket events, MCP tools
- **Backend README** (`backend/README.md`) — Build, test, config, deployment
- **TUI README** (`tui/README.md`) — Build and run the operator cockpit
- **Dashboard README** (`dashboard/README.md`) — Build and deploy the web UI

---

## Key Files at a Glance

| File | Purpose | Audience |
|------|---------|----------|
| QUICKSTART.md | Get running in 5 minutes | Traders, developers |
| ARCHITECTURE.md | How Hyperion works | Developers, architects |
| DESIGN.md | Why we chose this design | Developers |
| API.md | HTTP + MCP reference | Developers, integrators |

---

## FAQ

### I'm new to Hyperion. Where do I start?

→ **[QUICKSTART.md](QUICKSTART.md)** (5 min setup) → **[ARCHITECTURE.md](ARCHITECTURE.md)** (understand the system)

### I want to build or modify the backend. Where?

→ **[ARCHITECTURE.md](ARCHITECTURE.md)** (system design) + **[DESIGN.md](DESIGN.md)** (why we chose this design) + backend README (build instructions)

### I want to integrate Hyperion via MCP. How?

→ **[API.md](API.md)** section "MCP Protocol" + **[QUICKSTART.md](QUICKSTART.md)** step 7

### I need to understand the risk model. Where?

→ **[ARCHITECTURE.md](ARCHITECTURE.md)** section "Risk Architecture" (gates, scoped wallet, override)

### I want to understand the design tradeoffs. Why did you choose X over Y?

→ **[DESIGN.md](DESIGN.md)** (every major decision explained with context)

---

## Document Ownership

| Document | Owner | Status |
|----------|-------|--------|
| QUICKSTART.md | Engineering | Current |
| ARCHITECTURE.md | Engineering | Current |
| DESIGN.md | Product + Engineering | Current |
| API.md | Engineering | Current |

---

## Maintenance

### When to Update Docs

- **Architecture changes:** update ARCHITECTURE.md immediately
- **API changes:** update API.md + backend README
- **Design decisions:** add entry to DESIGN.md
- **Financial projections:** update FINANCIAL-PLAN.md annually
- **Pitch deck:** update only if core messaging changes (rare)

### Version Control

- All docs are in git
- Pitch copy (PITCH.md) is locked; changes require founder approval
- Investor docs are read-only once YC application is submitted (branching for updates)

---

## External Links

- **Landing page (live):** TBD (deploy pitch/deck/)
- **GitHub (optional):** TBD (public or private repo)
- **YC profile:** https://www.ycombinator.com/apply (submitted Jul 24, 2026)
- **Hyperliquid docs:** https://hyperliquid.gitbook.io/

---

## Latest Updates

**Jul 9, 2026:** Documentation structure finalized. All core docs complete.

- Added ARCHITECTURE.md, QUICKSTART.md, DESIGN.md, API.md
- Created investor/ folder with FINANCIAL-PLAN.md
- Organized pitch/ materials into deck/ subdirectory
- YC-APPLICATION.md timeline updated

---

## Contact

**Email:** nicolascerrato17@gmail.com

Questions about documentation? Open a GitHub issue or email the founders.

---

*Last updated: July 9, 2026*
