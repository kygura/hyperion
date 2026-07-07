# pitch

YC-application demo and landing-page assets for Hyperion.

## Demo

![Hyperion TUI showcase](media/hyperion-tui.gif)

The showcase covers the cockpit (`hyperagent-tui`) connecting to a live daemon, browsing the command surface (`/help`), growing the watchlist (`/watch add`), re-timeframing a market (`/tf`), flipping propose/autonomous execution mode, forcing a fresh scan (`/scan`), and asking the agent directly for a written read on a live market.

## Regenerating the demo

The demo is a reproducible VHS script. Install VHS:

```sh
go install github.com/charmbracelet/vhs@latest
```

Prerequisites: the `hyperagent` daemon must be running on `127.0.0.1:8787` with `-testnet` flag, warmed up with bars and a live WS link (`GET /api/health` reports `"connected": true`).

The tape's `Output` lines are repo-root-relative, so run it from the repo root:

```sh
vhs pitch/media/hyperion-tui.tape
```

This regenerates `pitch/media/hyperion-tui.gif` and `.mp4`.

## Landing page

`pitch.html` is the static hosted landing page; `PITCH.md` is the source copy it derives from. Both are authored and final — see the project PITCH for product messaging.
