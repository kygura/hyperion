# Cockpit TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the multi-view TUI in `tui/` with the four-panel cockpit from `pitch/mock-tui/`, wired to real daemon data, with chat as a bottom bar.

**Architecture:** New package `tui/internal/cockpit` holds a small Elm-style model + the mock's ported rendering. The tested data layer is reused unchanged: `tui/internal/apiclient` (HTTP client + cache) stays where it is; the WS bridge (`bridge.go`) moves into the cockpit package in the final flag-day task, when the old `internal/tui` package is deleted and `src/main.go` swaps over.

**Tech Stack:** Go 1.25, Bubbletea v2 (`charm.land/bubbletea/v2`), Bubbles v2, Lipgloss v2, `github.com/charmbracelet/x/ansi` (truncation), gorilla/websocket (via existing bridge).

**Spec:** `docs/superpowers/specs/2026-07-06-cockpit-tui-design.md`

## Global Constraints

- Work only inside `tui/` (plus `tui/README.md`). **No backend changes. `pitch/mock-tui/` stays untouched.**
- Bubbletea **v2** idioms only: `tea.KeyPressMsg` (not `tea.KeyMsg`), `viewport.New()` with no args, `textinput.New()`, `spinner.New(spinner.WithSpinner(...), spinner.WithStyle(...))`. Copy patterns from the existing `tui/internal/tui` files when unsure — they are v2.
- Do NOT import `github.com/charmbracelet/bubbletea` (v1) or `github.com/charmbracelet/lipgloss` (v1) — the mock uses those; every port must target `charm.land/*/v2`.
- Do NOT use `github.com/muesli/reflow/truncate` — use `ansi.Truncate(s, w, "…")` from `github.com/charmbracelet/x/ansi` (already a module dependency).
- Minimum terminal 96×28 (`minW`/`minH` guard, same message style as the mock).
- Journal ring cap: 200 entries.
- All code, comments, and copy in English. Conventional commits, **no AI attribution / no Co-Authored-By**.
- Run all commands from `/home/athan/projects/hypertrader/tui/`. Tests: `go test ./...`.
- The module must compile after every task (`go build ./...`).

---

### Task 1: Cockpit render helpers + palette

**Files:**
- Create: `tui/internal/cockpit/render.go`
- Test: `tui/internal/cockpit/render_test.go`

**Interfaces:**
- Consumes: nothing (pure functions).
- Produces (later tasks call these exactly): `box(title, rightTitle string, lines []string, w, h int) string`, `spread(l, r string, w int) string`, `padR(s string, w int) string`, `padL(s string, w int) string`, `signed(s string, v float64, w int) string`, `fnum(v float64, dec int) string`, `priceDec(v float64) int`, `bar(ratio float64, w int) string`, `truncTail(s string, w int) string`; style vars `logoStyle, textStyle, brightStyle, dimStyle, borderStyle, titleStyle, phaseStyle, greenStyle, redStyle, amberStyle, keyStyle` and `tagStyles map[string]lipgloss.Style`.

This is a near-verbatim port of `pitch/mock-tui/view.go` lines 13–55 and 238–338 (the palette, `box`, and formatting helpers) from lipgloss v1 to v2. The helpers use only `lipgloss.Width` + string building, which exists identically in v2.

- [ ] **Step 1: Write the failing test**

```go
package cockpit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestFnum(t *testing.T) {
	cases := []struct {
		v    float64
		dec  int
		want string
	}{
		{3412.4, 1, "3,412.4"},
		{67231, 0, "67,231"},
		{0.8412, 4, "0.8412"},
		{1234567.89, 2, "1,234,567.89"},
	}
	for _, c := range cases {
		if got := fnum(c.v, c.dec); got != c.want {
			t.Errorf("fnum(%v, %d) = %q, want %q", c.v, c.dec, got, c.want)
		}
	}
}

func TestPriceDec(t *testing.T) {
	cases := []struct {
		v    float64
		want int
	}{{0.5, 4}, {42, 2}, {3412, 1}, {67231, 0}}
	for _, c := range cases {
		if got := priceDec(c.v); got != c.want {
			t.Errorf("priceDec(%v) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestSpreadWidth(t *testing.T) {
	s := spread("left", "right", 40)
	if w := lipgloss.Width(s); w != 40 {
		t.Errorf("spread width = %d, want 40", w)
	}
}

func TestPad(t *testing.T) {
	if got := padR("ab", 5); got != "ab   " {
		t.Errorf("padR = %q", got)
	}
	if got := padL("ab", 5); got != "   ab" {
		t.Errorf("padL = %q", got)
	}
}

func TestBar(t *testing.T) {
	b := bar(0.5, 10)
	if w := lipgloss.Width(b); w != 10 {
		t.Errorf("bar width = %d, want 10", w)
	}
	if b2 := bar(1.5, 10); lipgloss.Width(b2) != 10 { // ratio clamped
		t.Errorf("bar clamp failed")
	}
}

func TestBoxGeometry(t *testing.T) {
	out := box("TITLE", "right", []string{"one", "two"}, 40, 8)
	rows := strings.Split(out, "\n")
	if len(rows) != 8 {
		t.Fatalf("box height = %d rows, want 8", len(rows))
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w != 40 {
			t.Errorf("row %d width = %d, want 40", i, w)
		}
	}
	if !strings.Contains(out, "TITLE") {
		t.Error("box missing title")
	}
}

func TestTruncTail(t *testing.T) {
	if got := truncTail("hello world", 6); lipgloss.Width(got) > 6 {
		t.Errorf("truncTail too wide: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -v`
Expected: FAIL (package does not exist / undefined functions).

- [ ] **Step 3: Write the implementation**

```go
// Package cockpit is the four-panel operator cockpit: the pitch mock's
// layout (pitch/mock-tui) rendered from real daemon data over the
// apiclient cache and WS bridge. Design:
// docs/superpowers/specs/2026-07-06-cockpit-tui-design.md
package cockpit

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The mock cockpit's fixed dark palette (pitch/mock-tui/view.go).
var (
	cAccent = lipgloss.Color("#2DE0A7")
	cText   = lipgloss.Color("#C9D4DE")
	cBright = lipgloss.Color("#EDF3F9")
	cDim    = lipgloss.Color("#5C6B7A")
	cBorder = lipgloss.Color("#28323D")
	cGreen  = lipgloss.Color("#4ADE80")
	cRed    = lipgloss.Color("#FF6B6B")
	cAmber  = lipgloss.Color("#F0B35B")
	cPurple = lipgloss.Color("#B48EF7")
	cCyan   = lipgloss.Color("#4FC1E9")

	logoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#06130D")).Background(cAccent).Bold(true)
	textStyle   = lipgloss.NewStyle().Foreground(cText)
	brightStyle = lipgloss.NewStyle().Foreground(cBright).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(cDim)
	borderStyle = lipgloss.NewStyle().Foreground(cBorder)
	titleStyle  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	phaseStyle  = lipgloss.NewStyle().Foreground(cAccent)
	greenStyle  = lipgloss.NewStyle().Foreground(cGreen)
	redStyle    = lipgloss.NewStyle().Foreground(cRed)
	amberStyle  = lipgloss.NewStyle().Foreground(cAmber)
	keyStyle    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)

	tagStyles = map[string]lipgloss.Style{
		"INGEST":   lipgloss.NewStyle().Foreground(cCyan).Bold(true),
		"REASON":   lipgloss.NewStyle().Foreground(cPurple).Bold(true),
		"EXECUTE":  lipgloss.NewStyle().Foreground(cAccent).Bold(true),
		"FILL":     lipgloss.NewStyle().Foreground(cGreen).Bold(true),
		"RISK":     lipgloss.NewStyle().Foreground(cAmber).Bold(true),
		"ERROR":    lipgloss.NewStyle().Foreground(cRed).Bold(true),
		"OPERATOR": lipgloss.NewStyle().Foreground(cRed).Bold(true),
	}
)

// box draws a rounded border with an embedded title, exactly h rows and
// w columns.
func box(title, rightTitle string, lines []string, w, h int) string {
	iw := w - 2 // width between the corner glyphs
	cw := iw - 2
	ch := h - 2

	t := " " + title + " "
	r := ""
	if rightTitle != "" {
		r = " " + rightTitle + " "
	}
	fill := iw - 1 - lipgloss.Width(t) - lipgloss.Width(r) - 1
	if fill < 0 {
		fill = 0
	}
	var b strings.Builder
	b.WriteString(borderStyle.Render("╭─") + titleStyle.Render(t) +
		borderStyle.Render(strings.Repeat("─", fill)) + dimStyle.Render(r) + borderStyle.Render("─╮"))

	for i := 0; i < ch; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		pad := cw - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("\n" + borderStyle.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + borderStyle.Render("│"))
	}

	b.WriteString("\n" + borderStyle.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return b.String()
}

// spread left-aligns l and right-aligns r within width w.
func spread(l, r string, w int) string {
	gap := w - lipgloss.Width(l) - lipgloss.Width(r)
	if gap < 1 {
		gap = 1
	}
	return l + strings.Repeat(" ", gap) + r
}

func padR(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func padL(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

// signed pads a numeric string to w then colors it green/red by sign.
func signed(s string, v float64, w int) string {
	if w > 0 {
		s = padL(s, w)
	}
	if v >= 0 {
		return greenStyle.Render(s)
	}
	return redStyle.Render(s)
}

// fnum formats with thousands separators.
func fnum(v float64, dec int) string {
	s := strconv.FormatFloat(v, 'f', dec, 64)
	ip, fp := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		ip, fp = s[:i], s[i:]
	}
	var b strings.Builder
	for j, c := range ip {
		if j > 0 && (len(ip)-j)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String() + fp
}

func priceDec(v float64) int {
	switch {
	case v < 1:
		return 4
	case v < 100:
		return 2
	case v < 10000:
		return 1
	default:
		return 0
	}
}

// bar renders a filled utilization bar of exactly w cells, ratio clamped
// to [0, 1].
func bar(ratio float64, w int) string {
	if w < 1 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	fill := int(ratio*float64(w) + 0.5)
	if fill > w {
		fill = w
	}
	return phaseStyle.Render(strings.Repeat("█", fill)) + dimStyle.Render(strings.Repeat("─", w-fill))
}

// truncTail truncates s to at most w display cells with a "…" tail.
func truncTail(s string, w int) string {
	if w < 1 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -v && go build ./...`
Expected: PASS, clean build.

- [ ] **Step 5: Commit**

```bash
git add tui/internal/cockpit/render.go tui/internal/cockpit/render_test.go
git commit -m "feat(tui): cockpit render helpers — mock palette and box layout on lipgloss v2"
```

---

### Task 2: Journal tag mapping + risk-envelope math

**Files:**
- Create: `tui/internal/cockpit/journal.go`
- Create: `tui/internal/cockpit/envelope.go`
- Test: `tui/internal/cockpit/journal_test.go`
- Test: `tui/internal/cockpit/envelope_test.go`

**Interfaces:**
- Consumes: `apiclient.Position` (fields `Coin, Size, EntryPrice, MarkPrice, UnrealPnl`, method `IsFlat()`), `apiclient.RiskSettings` (fields `MaxPositionUSD, MaxTotalExposureUSD, MaxConcurrent, DailyLossKillUSD`).
- Produces: `type journalEntry struct { at time.Time; tag, text string }`, `tagFor(kind string) string`, `appendJournal(entries []journalEntry, e journalEntry) []journalEntry`, `const maxJournal = 200`; `type envelope struct { ExposureUSD float64; OpenCount int; UPnL float64; Risk apiclient.RiskSettings }`, `computeEnvelope(positions []apiclient.Position, risk apiclient.RiskSettings) envelope`.

- [ ] **Step 1: Write the failing tests**

`journal_test.go`:

```go
package cockpit

import (
	"testing"
	"time"
)

func TestTagFor(t *testing.T) {
	cases := map[string]string{
		"candidate": "REASON",
		"open":      "EXECUTE",
		"close":     "EXECUTE",
		"fill":      "FILL",
		"alert":     "RISK",
		"error":     "ERROR",
		"":          "OPERATOR",
		"whatever":  "OPERATOR",
	}
	for kind, want := range cases {
		if got := tagFor(kind); got != want {
			t.Errorf("tagFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestAppendJournalCap(t *testing.T) {
	var entries []journalEntry
	for i := 0; i < maxJournal+50; i++ {
		entries = appendJournal(entries, journalEntry{at: time.Now(), tag: "FILL", text: "x"})
	}
	if len(entries) != maxJournal {
		t.Errorf("journal len = %d, want %d", len(entries), maxJournal)
	}
}
```

`envelope_test.go`:

```go
package cockpit

import (
	"testing"

	"github.com/hyperagent/tui/internal/apiclient"
)

func TestComputeEnvelope(t *testing.T) {
	risk := apiclient.RiskSettings{
		MaxPositionUSD: 5000, MaxTotalExposureUSD: 10000,
		MaxConcurrent: 3, DailyLossKillUSD: 500,
	}
	positions := []apiclient.Position{
		{Coin: "ETH", Size: 2, MarkPrice: 3400, UnrealPnl: 25.5},   // +6800 notional
		{Coin: "SOL", Size: -10, MarkPrice: 150, UnrealPnl: -4.5}, // +1500 notional (abs)
		{Coin: "BTC", Size: 0, MarkPrice: 67000, UnrealPnl: 0},    // flat — ignored
	}
	env := computeEnvelope(positions, risk)
	if env.ExposureUSD != 8300 {
		t.Errorf("ExposureUSD = %v, want 8300", env.ExposureUSD)
	}
	if env.OpenCount != 2 {
		t.Errorf("OpenCount = %d, want 2", env.OpenCount)
	}
	if env.UPnL != 21.0 {
		t.Errorf("UPnL = %v, want 21.0", env.UPnL)
	}
	if env.Risk != risk {
		t.Errorf("Risk not carried through")
	}
}

func TestComputeEnvelopeEmpty(t *testing.T) {
	env := computeEnvelope(nil, apiclient.RiskSettings{})
	if env.ExposureUSD != 0 || env.OpenCount != 0 || env.UPnL != 0 {
		t.Errorf("empty envelope not zero: %+v", env)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -run 'TagFor|Journal|Envelope' -v`
Expected: FAIL (undefined: tagFor, journalEntry, computeEnvelope).

- [ ] **Step 3: Write the implementation**

`journal.go`:

```go
package cockpit

import "time"

// maxJournal caps the decision-journal ring (mock's value).
const maxJournal = 200

// journalEntry is one line of the append-only decision journal.
type journalEntry struct {
	at   time.Time
	tag  string // REASON | EXECUTE | FILL | RISK | ERROR | OPERATOR
	text string
}

// tagFor maps a bus journal kind (bridge journalMsg.Kind) to the cockpit
// tag vocabulary. Unknown kinds — including operator-side notices that
// never came from the bus — read as OPERATOR.
func tagFor(kind string) string {
	switch kind {
	case "candidate":
		return "REASON"
	case "open", "close":
		return "EXECUTE"
	case "fill":
		return "FILL"
	case "alert":
		return "RISK"
	case "error":
		return "ERROR"
	}
	return "OPERATOR"
}

// appendJournal appends e and trims the ring to maxJournal.
func appendJournal(entries []journalEntry, e journalEntry) []journalEntry {
	entries = append(entries, e)
	if len(entries) > maxJournal {
		entries = entries[len(entries)-maxJournal:]
	}
	return entries
}
```

`envelope.go`:

```go
package cockpit

import "github.com/hyperagent/tui/internal/apiclient"

// envelope is the live risk-envelope utilization shown in the MANDATE and
// EXECUTION panels, computed client-side from open positions against the
// daemon's risk settings. Exposure is Σ|size × mark| over non-flat
// positions.
type envelope struct {
	ExposureUSD float64
	OpenCount   int
	UPnL        float64
	Risk        apiclient.RiskSettings
}

func computeEnvelope(positions []apiclient.Position, risk apiclient.RiskSettings) envelope {
	env := envelope{Risk: risk}
	for _, p := range positions {
		if p.IsFlat() {
			continue
		}
		notional := p.Size * p.MarkPrice
		if notional < 0 {
			notional = -notional
		}
		env.ExposureUSD += notional
		env.OpenCount++
		env.UPnL += p.UnrealPnl
	}
	return env
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/internal/cockpit/journal.go tui/internal/cockpit/journal_test.go tui/internal/cockpit/envelope.go tui/internal/cockpit/envelope_test.go
git commit -m "feat(tui): cockpit journal tag mapping and risk-envelope math"
```

---

### Task 3: Cockpit model, update loop, and panel views

**Files:**
- Create: `tui/internal/cockpit/msgs.go`
- Create: `tui/internal/cockpit/model.go`
- Create: `tui/internal/cockpit/update.go`
- Create: `tui/internal/cockpit/view.go`
- Test: `tui/internal/cockpit/model_test.go`
- Test: `tui/internal/cockpit/view_smoke_test.go`

**Interfaces:**
- Consumes: Task 1 helpers/styles, Task 2 `journalEntry`/`tagFor`/`appendJournal`/`computeEnvelope`; `apiclient.Cache` (methods `Mid(coin) float64`, `LatestBar(coin, tf) (Bar, bool)`, `AssetCtx(coin) (AssetCtx, bool)`, `Position(coin) Position`), `apiclient.Client` (method `SetMode(ctx, mode) error`), `apiclient.SettingsResponse` (fields `Mode, Visualized, Timeframes, Risk`).
- Produces: `type Model struct` with constructor `New(cfg Config) *Model` where `Config { Cache *apiclient.Cache; Controls *apiclient.Client; Settings apiclient.SettingsResponse; ChatFn ChatFunc }` and `type ChatFunc func(ctx context.Context, userMsg string, history []apiclient.ChatTurn) (string, error)`; message types in `msgs.go` (exact copies of the declarations currently in `tui/internal/tui/bridge.go` — Task 5 moves that file here and deletes its duplicate block): `statusKind` + `statusNotice`/`statusConn`, `barMsg`, `verdictMsg`, `journalMsg`, `statusMsg`, `positionMsg`, `chatReplyMsg`. Model fields Task 4 relies on: `chatOpen bool`, `busy bool`, `turns []apiclient.ChatTurn`, `input textinput.Model`, and method `submit(text string) tea.Cmd` (stub in this task, replaced in Task 4).

- [ ] **Step 1: Create `msgs.go` — copy the bridge message declarations verbatim**

Copy from `tui/internal/tui/bridge.go` (lines 33–74) exactly, into package `cockpit`:

```go
package cockpit

import (
	"github.com/hyperagent/tui/internal/apiclient"
)

// statusKind discriminates what a statusMsg is asserting, so consumers read
// only the fields that event owns — mirrors backend/internal/bus.StatusKind's
// two values locally (bus is backend-internal and this module cannot import
// it).
type statusKind int

const (
	// statusNotice is a transient message (reasoner error, history-write
	// failure). It carries Detail and optionally Provider; it must not touch
	// connection state.
	statusNotice statusKind = iota
	// statusConn asserts the websocket connection state via Connected.
	statusConn
)

// Tea messages the render loop reacts to, mirroring the shape of
// backend/internal/bus events. PumpWS produces these from real server push
// frames.
type (
	barMsg     apiclient.Bar
	verdictMsg apiclient.Verdict

	// journalMsg mirrors backend/internal/bus.JournalEvent.
	journalMsg struct {
		Coin    string
		Kind    string // "candidate" | "fill" | "open" | "close" | "alert" | "error"
		Summary string
		Verdict *apiclient.Verdict // non-nil for candidate events
	}

	// statusMsg mirrors backend/internal/bus.StatusEvent.
	statusMsg struct {
		Kind      statusKind
		Connected bool // authoritative only when Kind == statusConn
		Provider  string
		Mode      string // "propose" | "autonomous"
		Detail    string
	}

	positionMsg apiclient.Position

	chatReplyMsg struct {
		text string
		err  error
	}
)
```

- [ ] **Step 2: Write the failing tests**

`model_test.go`:

```go
package cockpit

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

func testModel() *Model {
	cache := apiclient.NewCache()
	return New(Config{
		Cache: cache,
		Settings: apiclient.SettingsResponse{
			Mode:       "propose",
			Visualized: []string{"ETH", "BTC"},
			Timeframes: map[string]string{"ETH": "1h"},
			Risk: apiclient.RiskSettings{
				MaxPositionUSD: 5000, MaxTotalExposureUSD: 10000,
				MaxConcurrent: 3, DailyLossKillUSD: 500,
			},
		},
	})
}

func TestJournalMsgAppendsTaggedEntry(t *testing.T) {
	m := testModel()
	m.Update(journalMsg{Coin: "ETH", Kind: "fill", Summary: "0.85 ETH filled"})
	if len(m.journal) != 1 {
		t.Fatalf("journal len = %d, want 1", len(m.journal))
	}
	if m.journal[0].tag != "FILL" {
		t.Errorf("tag = %q, want FILL", m.journal[0].tag)
	}
	if m.phase != "FILL" {
		t.Errorf("phase = %q, want FILL", m.phase)
	}
}

func TestStatusConnUpdatesConnected(t *testing.T) {
	m := testModel()
	m.Update(statusMsg{Kind: statusConn, Connected: true})
	if !m.connected {
		t.Error("connected not set")
	}
	m.Update(statusMsg{Kind: statusConn, Connected: false})
	if m.connected {
		t.Error("connected not cleared")
	}
}

func TestStatusNoticeUpdatesModeNotConn(t *testing.T) {
	m := testModel()
	m.connected = true
	m.Update(statusMsg{Kind: statusNotice, Mode: "autonomous", Detail: "mode → autonomous"})
	if m.mode != "autonomous" {
		t.Errorf("mode = %q, want autonomous", m.mode)
	}
	if !m.connected {
		t.Error("notice must not touch connection state")
	}
}

func TestVerdictMsgAppendsReason(t *testing.T) {
	m := testModel()
	m.Update(verdictMsg{Asset: "ETH", Action: apiclient.ActionOpenLong, Confidence: 0.7, Thesis: "funding favors bids"})
	if len(m.journal) != 1 || m.journal[0].tag != "REASON" {
		t.Fatalf("verdict not journaled as REASON: %+v", m.journal)
	}
}

func TestQuitKey(t *testing.T) {
	m := testModel()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q should produce a quit command")
	}
}

func TestSlashOpensChat(t *testing.T) {
	m := testModel()
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.chatOpen {
		t.Error("/ should open chat")
	}
}
```

Note: if `tea.KeyPressMsg{Code: 'q', Text: "q"}` fails to compile, check how the existing tests construct key presses — `grep -rn "KeyPressMsg{" tui/internal/tui/*_test.go` — and use that exact form.

`view_smoke_test.go`:

```go
package cockpit

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

func TestViewSmoke(t *testing.T) {
	cache := apiclient.NewCache()
	cache.PutMid("ETH", 3412.4)
	cache.PutBar(apiclient.Bar{
		Coin: "ETH", Timeframe: "1h", CloseTime: time.Now(),
		Open: 3400, Close: 3412.4, Funding: 0.0000125, OIDelta: 0.009, CVD: 4.2e6,
	})
	cache.ApplyMarkets([]apiclient.MarketEntry{{
		Coin:     "ETH",
		Mid:      3412.4,
		Position: apiclient.Position{Coin: "ETH", Size: 2.44, EntryPrice: 3402.1, MarkPrice: 3412.4, UnrealPnl: 25.1},
	}})

	m := New(Config{
		Cache: cache,
		Settings: apiclient.SettingsResponse{
			Mode:       "propose",
			Visualized: []string{"ETH"},
			Timeframes: map[string]string{"ETH": "1h"},
			Risk:       apiclient.RiskSettings{MaxPositionUSD: 5000, MaxTotalExposureUSD: 10000, MaxConcurrent: 3, DailyLossKillUSD: 500},
		},
	})
	m.Update(journalMsg{Coin: "ETH", Kind: "fill", Summary: "0.85 ETH filled @ 3,391.50"})
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})

	out := m.View()
	for _, want := range []string{"MANDATE", "MARKET PICTURE", "EXECUTION", "DECISION JOURNAL", "HYPERTRADER", "ETH"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if rows := strings.Count(out, "\n") + 1; rows != 30 {
		t.Errorf("view rows = %d, want 30", rows)
	}
}

func TestViewTooSmall(t *testing.T) {
	m := New(Config{Cache: apiclient.NewCache()})
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 10})
	if out := m.View(); !strings.Contains(out, "needs at least") {
		t.Error("small-terminal guard missing")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -v`
Expected: FAIL (undefined: Model, New, Config).

- [ ] **Step 4: Write `model.go`**

```go
package cockpit

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// ChatFunc runs an interactive completion against the daemon's /api/chat
// endpoint. Called inside a tea.Cmd so the render loop never blocks on the
// LLM.
type ChatFunc func(ctx context.Context, userMsg string, history []apiclient.ChatTurn) (reply string, err error)

// Config carries everything the cockpit needs at construction.
type Config struct {
	Cache    *apiclient.Cache
	Controls *apiclient.Client
	Settings apiclient.SettingsResponse
	ChatFn   ChatFunc
}

// Model is the cockpit root model: one screen, four panels, a chat bar.
type Model struct {
	width, height int

	cache    *apiclient.Cache
	controls *apiclient.Client
	chatFn   ChatFunc

	visualized []string
	timeframes map[string]string // coin -> display timeframe (default 1h)
	risk       apiclient.RiskSettings

	mode      string // "propose" | "autonomous"
	connected bool
	phase     string // last journal tag, shown in the header
	startedAt time.Time

	journal []journalEntry

	// Chat: bottom input bar; when open, the reply pane replaces the
	// DECISION JOURNAL panel.
	chatOpen bool
	busy     bool
	turns    []apiclient.ChatTurn
	input    textinput.Model

	spin spinner.Model
}

// New builds the cockpit model from the startup settings snapshot.
func New(cfg Config) *Model {
	ti := textinput.New()
	ti.Placeholder = "ask the agent… (/help for commands)"
	ti.Prompt = "> "

	sp := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(phaseStyle),
	)

	tf := make(map[string]string, len(cfg.Settings.Timeframes))
	for k, v := range cfg.Settings.Timeframes {
		tf[k] = v
	}

	return &Model{
		cache:      cfg.Cache,
		controls:   cfg.Controls,
		chatFn:     cfg.ChatFn,
		visualized: cfg.Settings.Visualized,
		timeframes: tf,
		risk:       cfg.Settings.Risk,
		mode:       cfg.Settings.Mode,
		phase:      "INGEST",
		startedAt:  time.Now(),
		input:      ti,
		spin:       sp,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, textinput.Blink)
}

// tf returns the display timeframe for coin (default "1h").
func (m *Model) tf(coin string) string {
	if t, ok := m.timeframes[coin]; ok && t != "" {
		return t
	}
	return "1h"
}

// envelope computes live risk utilization from the visualized watchlist's
// cached positions.
func (m *Model) envelope() envelope {
	positions := make([]apiclient.Position, 0, len(m.visualized))
	for _, coin := range m.visualized {
		positions = append(positions, m.cache.Position(coin))
	}
	return computeEnvelope(positions, m.risk)
}

// note appends an operator-side journal entry (not from the bus).
func (m *Model) note(tag, text string) {
	m.phase = tag
	m.journal = appendJournal(m.journal, journalEntry{at: time.Now(), tag: tag, text: text})
}
```

- [ ] **Step 5: Write `update.go`**

```go
package cockpit

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case barMsg, positionMsg:
		// Data already applied to the cache by the bridge; repaint happens
		// because a message arrived.
		return m, nil

	case journalMsg:
		text := msg.Summary
		if msg.Coin != "" {
			text = msg.Coin + " — " + msg.Summary
		}
		m.phase = tagFor(msg.Kind)
		m.journal = appendJournal(m.journal, journalEntry{at: timeNow(), tag: tagFor(msg.Kind), text: text})
		return m, nil

	case verdictMsg:
		text := fmt.Sprintf("%s %s", msg.Asset, msg.Action)
		if msg.Confidence > 0 {
			text += fmt.Sprintf(" %.0f%%", msg.Confidence*100)
		}
		if msg.Thesis != "" {
			text += " — " + msg.Thesis
		}
		m.phase = "REASON"
		m.journal = appendJournal(m.journal, journalEntry{at: timeNow(), tag: "REASON", text: text})
		return m, nil

	case statusMsg:
		switch msg.Kind {
		case statusConn:
			m.connected = msg.Connected
		case statusNotice:
			if msg.Mode != "" {
				m.mode = msg.Mode
			}
			if msg.Detail != "" {
				m.note("OPERATOR", msg.Detail)
			}
		}
		return m, nil

	case chatReplyMsg:
		m.busy = false
		if msg.err != nil {
			m.turns = append(m.turns, chatTurn("system", "error: "+msg.err.Error()))
		} else {
			m.turns = append(m.turns, chatTurn("assistant", msg.text))
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.input.Focused() {
		switch msg.String() {
		case "esc":
			m.input.Blur()
			m.chatOpen = false
			return m, nil
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			if text == "" {
				return m, nil
			}
			return m, m.submit(text)
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "m":
		return m, m.toggleMode()
	case "/":
		m.chatOpen = true
		return m, m.input.Focus()
	case "esc":
		m.chatOpen = false
	}
	return m, nil
}

// toggleMode flips propose <-> autonomous via the daemon's control API. The
// authoritative mode value comes back as a statusMsg (either the push
// stream's or the one this cmd fabricates on success).
func (m *Model) toggleMode() tea.Cmd {
	if m.controls == nil {
		return nil
	}
	next := "autonomous"
	if m.mode == "autonomous" {
		next = "propose"
	}
	c := m.controls
	return func() tea.Msg {
		if err := c.SetMode(context.Background(), next); err != nil {
			return journalMsg{Kind: "error", Summary: "mode switch failed: " + err.Error()}
		}
		return statusMsg{Kind: statusNotice, Mode: next, Detail: "mode → " + next}
	}
}

// submit routes the chat bar's input; Task 4 replaces this stub with the
// command dispatcher + chat call.
func (m *Model) submit(text string) tea.Cmd {
	m.turns = append(m.turns, chatTurn("user", text))
	return nil
}
```

Also add to `model.go` (or a small `util.go` in the package):

```go
import "time" // already imported in model.go

// timeNow is a seam for tests.
var timeNow = time.Now

func chatTurn(role, text string) apiclient.ChatTurn {
	return apiclient.ChatTurn{Role: role, Text: text}
}
```

- [ ] **Step 6: Write `view.go`**

Layout constants and chrome mirror the mock (`pitch/mock-tui/view.go` lines 13–108) with one extra chrome row for the chat bar:

```go
package cockpit

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

const (
	minW = 96
	minH = 28

	leftColW = 42
	topRowH  = 12
	chromeH  = 3 // header + chat bar + footer
)

// View implements tea.Model.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minW || m.height < minH {
		msg := dimStyle.Render(fmt.Sprintf("hypertrader needs at least %d×%d — current %d×%d", minW, minH, m.width, m.height))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	bodyH := m.height - chromeH
	botRowH := bodyH - topRowH
	rightColW := m.width - leftColW

	top := lipgloss.JoinHorizontal(lipgloss.Top,
		m.mandateView(leftColW, topRowH),
		m.marketsView(rightColW, topRowH),
	)
	rightBot := m.journalView(rightColW, botRowH)
	if m.chatOpen {
		rightBot = m.chatView(rightColW, botRowH)
	}
	bot := lipgloss.JoinHorizontal(lipgloss.Top,
		m.positionsView(leftColW, botRowH),
		rightBot,
	)

	return m.headerView() + "\n" + top + "\n" + bot + "\n" + m.chatBarView() + "\n" + m.footerView()
}

func (m *Model) headerView() string {
	left := logoStyle.Render(" HYPERTRADER ") +
		dimStyle.Render("  autonomous trading operator · Hyperliquid")

	up := time.Since(m.startedAt)
	uptime := dimStyle.Render(fmt.Sprintf("up %dh %02dm  ", int(up.Hours()), int(up.Minutes())%60))

	loop := m.spin.View() + " " + phaseStyle.Render("LOOP · "+m.phase)

	status := amberStyle.Bold(true).Render("● DISCONNECTED")
	if m.connected {
		status = greenStyle.Bold(true).Render("● CONNECTED")
	}

	modeChip := amberStyle.Bold(true).Render("PROPOSE")
	if m.mode == "autonomous" {
		modeChip = redStyle.Bold(true).Render("AUTONOMOUS")
	}

	return spread(left, uptime+loop+"   "+modeChip+" "+status+" ", m.width)
}

func (m *Model) footerView() string {
	keys := " " + keyStyle.Render("/") + dimStyle.Render(" chat   ") +
		keyStyle.Render("m") + dimStyle.Render(" mode   ") +
		keyStyle.Render("q") + dimStyle.Render(" quit")
	note := dimStyle.Italic(true).Render("every decision in writing · connected to live daemon ")
	return spread(keys, note, m.width)
}

func (m *Model) chatBarView() string {
	if m.input.Focused() {
		return " " + m.input.View()
	}
	if m.busy {
		return " " + m.spin.View() + dimStyle.Render(" agent is thinking…")
	}
	return " " + dimStyle.Render("› press / to talk to the agent")
}

func (m *Model) mandateView(w, h int) string {
	cw := w - 4
	env := m.envelope()
	var lines []string

	lines = append(lines, dimStyle.Render(padR("MODE", 12))+brightStyle.Render(strings.ToUpper(m.mode)))
	lines = append(lines, "")

	lines = append(lines, dimStyle.Render(padR("EXPOSURE", 12))+
		brightStyle.Render("$"+fnum(env.ExposureUSD, 0))+
		dimStyle.Render("  / $"+fnum(env.Risk.MaxTotalExposureUSD, 0)+" cap"))
	ratio := 0.0
	if env.Risk.MaxTotalExposureUSD > 0 {
		ratio = env.ExposureUSD / env.Risk.MaxTotalExposureUSD
	}
	lines = append(lines, bar(ratio, cw))
	lines = append(lines, "")

	lines = append(lines, envelopeLine("POSITIONS", fmt.Sprintf("%d", env.OpenCount),
		fmt.Sprintf("/ %d max", env.Risk.MaxConcurrent), env.OpenCount <= env.Risk.MaxConcurrent, cw))
	lines = append(lines, envelopeLine("MAX POS", "$"+fnum(env.Risk.MaxPositionUSD, 0), "per position", true, cw))
	lines = append(lines, envelopeLine("uPnL", fmt.Sprintf("%+.2f", env.UPnL), "unrealized", env.UPnL >= 0, cw))
	lines = append(lines, envelopeLine("KILL-SWITCH", "$"+fnum(env.Risk.DailyLossKillUSD, 0), "daily loss · armed", true, cw))

	return box("MANDATE", "risk envelope", lines, w, h)
}

func envelopeLine(label, val, extra string, ok bool, cw int) string {
	left := dimStyle.Render(padR(label, 12)) + brightStyle.Render(val) + dimStyle.Render(" "+extra)
	state := greenStyle.Render("● ok")
	if !ok {
		state = redStyle.Render("● breach")
	}
	return spread(left, state, cw)
}

func (m *Model) marketsView(w, h int) string {
	var lines []string
	lines = append(lines, dimStyle.Render(padR("MKT", 5)+"  "+padL("LAST", 10)+"  "+
		padL("FUND/8H", 9)+"  "+padL("OIΔ", 7)+"  "+padL("CVD", 8)))

	for _, coin := range m.visualized {
		mid := m.cache.Mid(coin)
		var funding, oiDelta, cvd float64
		if ctx, ok := m.cache.AssetCtx(coin); ok {
			funding = ctx.Funding * 100
		}
		if b, ok := m.cache.LatestBar(coin, m.tf(coin)); ok {
			oiDelta = b.OIDelta
			cvd = b.CVD / 1e6
		}
		row := brightStyle.Render(padR(coin, 5)) + "  " +
			textStyle.Render(padL(fnum(mid, priceDec(mid)), 10)) + "  " +
			signed(fmt.Sprintf("%+.4f%%", funding), funding, 9) + "  " +
			signed(fmt.Sprintf("%+.2f", oiDelta), oiDelta, 7) + "  " +
			signed(fmt.Sprintf("%+.1fM", cvd), cvd, 8)
		lines = append(lines, row)
	}

	return box("MARKET PICTURE", "live ingest", lines, w, h)
}

func (m *Model) positionsView(w, h int) string {
	cw := w - 4
	env := m.envelope()
	var lines []string

	lines = append(lines, dimStyle.Render("OPEN POSITIONS"))
	open := 0
	for _, coin := range m.visualized {
		p := m.cache.Position(coin)
		if p.IsFlat() {
			continue
		}
		open++
		side, sideStyle := "LONG", greenStyle
		if p.IsShort() {
			side, sideStyle = "SHORT", redStyle
		}
		size := p.Size
		if size < 0 {
			size = -size
		}
		lines = append(lines,
			brightStyle.Render(padR(p.Coin, 10))+
				sideStyle.Bold(true).Render(side)+
				textStyle.Render(fmt.Sprintf(" %.2f @ %s", size, fnum(p.EntryPrice, priceDec(p.EntryPrice)))))
		lines = append(lines, spread(
			dimStyle.Render("  uPnL ")+signed(fmt.Sprintf("%+.2f", p.UnrealPnl), p.UnrealPnl, 0),
			dimStyle.Render("mark "+fnum(p.MarkPrice, priceDec(p.MarkPrice))), cw))
	}
	if open == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("flat — no open positions"))
	}
	lines = append(lines, "")

	// Compiled risk gates, pass/state derived from live utilization.
	pass := 0
	type gateRow struct {
		name string
		ok   bool
	}
	maxPosOK := true
	for _, coin := range m.visualized {
		p := m.cache.Position(coin)
		if p.IsFlat() {
			continue
		}
		n := p.Size * p.MarkPrice
		if n < 0 {
			n = -n
		}
		if env.Risk.MaxPositionUSD > 0 && n > env.Risk.MaxPositionUSD {
			maxPosOK = false
		}
	}
	gates := []gateRow{
		{fmt.Sprintf("max position $%s", fnum(env.Risk.MaxPositionUSD, 0)), maxPosOK},
		{fmt.Sprintf("max exposure $%s", fnum(env.Risk.MaxTotalExposureUSD, 0)), env.Risk.MaxTotalExposureUSD == 0 || env.ExposureUSD <= env.Risk.MaxTotalExposureUSD},
		{fmt.Sprintf("max concurrency %d/%d", env.OpenCount, env.Risk.MaxConcurrent), env.Risk.MaxConcurrent == 0 || env.OpenCount <= env.Risk.MaxConcurrent},
		{fmt.Sprintf("daily-loss kill-switch $%s · armed", fnum(env.Risk.DailyLossKillUSD, 0)), true},
	}
	for _, g := range gates {
		if g.ok {
			pass++
		}
	}
	lines = append(lines, spread(dimStyle.Render("RISK GATES — compiled"),
		titleStyle.Render(fmt.Sprintf("%d/%d PASS", pass, len(gates))), cw))
	for _, g := range gates {
		mark := greenStyle.Render("✓ ")
		if !g.ok {
			mark = redStyle.Render("✗ ")
		}
		lines = append(lines, mark+textStyle.Render(g.name))
	}

	return box("EXECUTION", "compiled gates", lines, w, h)
}

func (m *Model) journalView(w, h int) string {
	cw := w - 4
	ch := h - 2
	bodyW := cw - 18 // timestamp (8) + gap + tag (8) + gap

	start := len(m.journal) - ch
	if start < 0 {
		start = 0
	}

	var lines []string
	for _, e := range m.journal[start:] {
		tag, ok := tagStyles[e.tag]
		if !ok {
			tag = dimStyle
		}
		body := textStyle.Render(truncTail(e.text, bodyW))
		if e.tag == "OPERATOR" {
			body = amberStyle.Render(truncTail(e.text, bodyW))
		}
		lines = append(lines,
			dimStyle.Render(e.at.Format("15:04:05"))+" "+tag.Render(padR(e.tag, 8))+" "+body)
	}

	right := fmt.Sprintf("append-only · %d", len(m.journal))
	return box("DECISION JOURNAL", right, lines, w, h)
}

// chatView renders the agent conversation in place of the journal panel.
func (m *Model) chatView(w, h int) string {
	cw := w - 4
	ch := h - 2

	var lines []string
	for _, t := range m.turns {
		switch t.Role {
		case "user":
			lines = append(lines, keyStyle.Render("you  ")+textStyle.Render(truncTail(t.Text, cw-5)))
		case "system":
			for _, l := range strings.Split(t.Text, "\n") {
				lines = append(lines, dimStyle.Render("  "+truncTail(l, cw-2)))
			}
		default: // assistant
			lines = append(lines, greenStyle.Bold(true).Render("agent"))
			for _, l := range strings.Split(wordWrap(t.Text, cw-2), "\n") {
				lines = append(lines, textStyle.Render("  "+l))
			}
		}
	}
	if m.busy {
		lines = append(lines, m.spin.View()+dimStyle.Render(" thinking…"))
	}
	if len(lines) > ch {
		lines = lines[len(lines)-ch:]
	}

	return box("AGENT", "esc to close", lines, w, h)
}

// wordWrap wraps s to at most width characters per line, breaking on spaces.
func wordWrap(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}
	var out strings.Builder
	for len(s) > width {
		cut := strings.LastIndex(s[:width], " ")
		if cut <= 0 {
			cut = width
		}
		out.WriteString(s[:cut])
		out.WriteByte('\n')
		s = strings.TrimLeft(s[cut:], " ")
	}
	out.WriteString(s)
	return out.String()
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -v && go build ./... && go vet ./internal/cockpit/`
Expected: PASS, clean build. If a Bubbletea v2 API call fails to compile, find the working idiom in `tui/internal/tui/` (it is all v2) and match it — do not downgrade any import to v1.

- [ ] **Step 8: Commit**

```bash
git add tui/internal/cockpit/
git commit -m "feat(tui): cockpit model, update loop, and four-panel view on real data"
```

---

### Task 4: Chat bar — command dispatcher + agent chat

**Files:**
- Create: `tui/internal/cockpit/commands.go`
- Modify: `tui/internal/cockpit/update.go` (replace the `submit` stub)
- Test: `tui/internal/cockpit/commands_test.go`

**Interfaces:**
- Consumes: `Model.turns/busy/chatOpen/input` (Task 3), `apiclient.Client` methods `Scan(ctx, coins...) error`, `Subscribe(ctx, coins...) error`, `Track(ctx, coin, timeframe) error`, `Untrack(ctx, coin) error`, `SetMode(ctx, mode) error`; `m.chatFn` for free-text messages.
- Produces: `isCommand(s string) bool`, `(m *Model) runCommand(input string) (string, tea.Cmd)`, `(m *Model) submit(text string) tea.Cmd` (final version), `(m *Model) sendChat(text string) tea.Cmd`.
- Reference: port the argument-parsing helpers from `tui/internal/tui/commands.go` (`splitOp`, `upperAll`, `argOr`) — copy them verbatim; they are pure. Surviving commands: `/scan /watch /track /tf /mode /clear /help`. Dropped (do NOT port): `/settings /keys /provider /model /g`.

- [ ] **Step 1: Write the failing tests**

```go
package cockpit

import (
	"strings"
	"testing"

	"github.com/hyperagent/tui/internal/apiclient"
)

func TestIsCommand(t *testing.T) {
	if !isCommand("/scan") {
		t.Error("/scan should be a command")
	}
	if isCommand("what is funding?") {
		t.Error("free text is not a command")
	}
}

func TestRunCommandHelp(t *testing.T) {
	m := testModel()
	out, cmd := m.runCommand("/help")
	if cmd != nil {
		t.Error("/help should be local (nil cmd)")
	}
	for _, want := range []string{"/scan", "/watch", "/track", "/tf", "/mode", "/clear"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestRunCommandClear(t *testing.T) {
	m := testModel()
	m.turns = []apiclient.ChatTurn{{Role: "user", Text: "hi"}}
	out, _ := m.runCommand("/clear")
	if len(m.turns) != 0 {
		t.Error("/clear should empty the conversation")
	}
	if out == "" {
		t.Error("/clear should confirm")
	}
}

func TestRunCommandUnknown(t *testing.T) {
	m := testModel()
	out, _ := m.runCommand("/bogus")
	if !strings.Contains(out, "unknown") {
		t.Errorf("unknown command not reported: %q", out)
	}
}

func TestSubmitFreeTextGoesToChat(t *testing.T) {
	m := testModel()
	m.chatFn = func(ctx contextArg, msg string, h []apiclient.ChatTurn) (string, error) {
		return "reply", nil
	}
	cmd := m.submit("what is funding?")
	if !m.busy {
		t.Error("free text should set busy")
	}
	if cmd == nil {
		t.Fatal("free text should produce a chat cmd")
	}
	if len(m.turns) == 0 || m.turns[len(m.turns)-1].Role != "user" {
		t.Error("user turn not recorded")
	}
}

func TestSubmitCommandRecordsSystemTurn(t *testing.T) {
	m := testModel()
	m.submit("/help")
	last := m.turns[len(m.turns)-1]
	if last.Role != "system" {
		t.Errorf("command output role = %q, want system", last.Role)
	}
}
```

Note: `contextArg` above is shorthand — write the real signature `func(ctx context.Context, msg string, h []apiclient.ChatTurn) (string, error)` with `"context"` imported.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -run 'Command|Submit' -v`
Expected: FAIL (undefined: isCommand, runCommand).

- [ ] **Step 3: Write `commands.go` and the final `submit`**

`commands.go` — port from `tui/internal/tui/commands.go`, keeping only the surviving commands. Structure:

```go
package cockpit

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// isCommand reports whether s is a slash command.
func isCommand(s string) bool { return strings.HasPrefix(s, "/") }

// runCommand executes a slash command locally and/or returns a tea.Cmd for
// the network half. The string return is the immediate system output.
func runCommand / cmdWatch / cmdTrack / cmdTimeframe / cmdMode / commandHelp / splitOp / upperAll / argOr
```

Porting rules:
- Open `tui/internal/tui/commands.go` and copy the bodies of `runCommand` (cases `scan`, `watch`, `track`, `tf`/`timeframe`, `mode`, `clear`, `help` only), `cmdWatch`, `cmdTrack`, `cmdTimeframe`, `cmdMode`, `commandHelp`, `splitOp`, `upperAll`, `argOr`, adapting field names to the cockpit Model (`m.controls`, `m.visualized`, `m.timeframes`, `m.turns`). Where the old code touched dropped state (`m.tracked` map, overlays, `m.settings` refresh, ideas board), delete those lines. Network calls stay wrapped in `tea.Cmd` closures exactly as the old code does (each returns a `journalMsg{Kind: "error", ...}` on failure or a `statusMsg`/nil on success — follow the old pattern, substituting the cockpit message types, which have identical shapes).
- `/clear` sets `m.turns = nil` and returns "conversation cleared".
- `commandHelp()` must list exactly: `/scan /watch /track /tf /mode /clear /help` with one-line descriptions.
- Unknown command returns `"unknown command — /help lists commands"`.

Final `submit` + `sendChat` in `update.go` (replace the Task 3 stub):

```go
// submit routes the chat bar's input: slash commands run through the
// dispatcher and record their output as a system turn; anything else is a
// user turn sent to the agent.
func (m *Model) submit(text string) tea.Cmd {
	if isCommand(text) {
		m.turns = append(m.turns, chatTurn("user", text))
		out, cmd := m.runCommand(text)
		if out != "" {
			m.turns = append(m.turns, chatTurn("system", out))
		}
		return cmd
	}
	m.turns = append(m.turns, chatTurn("user", text))
	if m.chatFn == nil {
		m.turns = append(m.turns, chatTurn("system", "chat unavailable — no daemon connection"))
		return nil
	}
	m.busy = true
	return tea.Batch(m.sendChat(text), m.spin.Tick)
}

// sendChat calls the daemon chat endpoint off the render loop.
func (m *Model) sendChat(text string) tea.Cmd {
	history := append([]apiclient.ChatTurn(nil), m.turns...)
	fn := m.chatFn
	return func() tea.Msg {
		reply, err := fn(context.Background(), text, history)
		return chatReplyMsg{text: reply, err: err}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tui/internal/cockpit/
git commit -m "feat(tui): cockpit chat bar — trimmed slash commands and agent chat"
```

---

### Task 5: Flag day — move the bridge, swap main, delete the old TUI

**Files:**
- Move: `tui/internal/tui/bridge.go` → `tui/internal/cockpit/bridge.go`
- Move: `tui/internal/tui/bridge_test.go` → `tui/internal/cockpit/bridge_test.go`
- Modify: `tui/src/main.go` (full rewrite below)
- Delete: everything else under `tui/internal/tui/`
- Modify: `tui/README.md`

**Interfaces:**
- Consumes: `cockpit.New/Config/ChatFunc` (Task 3/4), `cockpit.PumpWS` + `cockpit.PollMarkets` (arrive with the moved bridge), `apiclient.New/NewCache/Client.Settings/Client.Chat`.
- Produces: the shipping `hyperagent-tui` binary.

- [ ] **Step 1: Move the bridge**

```bash
cd /home/athan/projects/hypertrader
git mv tui/internal/tui/bridge.go tui/internal/cockpit/bridge.go
git mv tui/internal/tui/bridge_test.go tui/internal/cockpit/bridge_test.go
```

In both moved files change `package tui` to `package cockpit`. Then in `bridge.go` delete the declarations that Task 3's `msgs.go` already owns — the `statusKind` type + `statusNotice`/`statusConn` const block and the entire `type ( barMsg ... chatReplyMsg )` block (in the original file these are lines 28–74, from the `statusKind` comment through the closing paren of the message-type group). Keep the `Sender` interface, `wsFrame`, `healthyConnDuration`, `nextBackoff`, `sleepCtx`, `PumpWS`, `readLoop`, `wsURLFrom`, `PollMarkets`. Fix `bridge_test.go` compile errors the same way (it may reference deleted helpers from the old package — if it references `Model` internals that no longer exist, adapt the assertions to the cockpit `Model`; the reconnect-backoff tests are pure and must survive unchanged).

- [ ] **Step 2: Verify the cockpit package still passes**

Run: `cd /home/athan/projects/hypertrader/tui && go test ./internal/cockpit/ -v`
Expected: PASS (old `internal/tui` package is now broken — that is expected and fixed by Step 4).

- [ ] **Step 3: Rewrite `tui/src/main.go`**

```go
// Command hyperagent-tui is the standalone cockpit client for the
// hyperagent daemon: it holds no backend state of its own, talking
// exclusively over HTTP+WS to a running daemon's unified core API.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
	"github.com/hyperagent/tui/internal/cockpit"
)

func main() {
	coreURL := flag.String("core-url", "http://127.0.0.1:8787", "hyperagent daemon base URL")
	token := flag.String("token", os.Getenv("HYPERAGENT_TOKEN"), "bearer token, if the daemon requires one")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	client := apiclient.New(*coreURL, *token)
	cache := apiclient.NewCache()

	settings, err := client.Settings(ctx)
	if err != nil {
		log.Fatalf("hyperagent-tui: could not reach daemon at %s: %v", *coreURL, err)
	}

	chatFn := func(ctx context.Context, msg string, history []apiclient.ChatTurn) (string, error) {
		reply, _, _, err := client.Chat(ctx, msg, history)
		return reply, err
	}

	model := cockpit.New(cockpit.Config{
		Cache:    cache,
		Controls: client,
		Settings: settings,
		ChatFn:   chatFn,
	})

	p := tea.NewProgram(model, tea.WithContext(ctx))
	go cockpit.PumpWS(ctx, *coreURL, cache, p)
	go cockpit.PollMarkets(ctx, client, cache, p)

	if _, err := p.Run(); err != nil {
		cancel()
		log.Fatalf("hyperagent-tui: %v", err)
	}
	cancel()
}
```

- [ ] **Step 4: Delete the old package and tidy**

```bash
cd /home/athan/projects/hypertrader
git rm -r tui/internal/tui/
cd tui && go mod tidy && go build ./... && go test ./... && go vet ./...
```

Expected: clean build, all tests pass (`internal/apiclient` + `internal/cockpit` only). If `go mod tidy` drops `charm.land/glamour/v2` (markdown was only used by the old chat pane), that is correct.

- [ ] **Step 5: Update `tui/README.md`**

Rewrite the UI-description sections to match the cockpit: four panels (MANDATE risk envelope · MARKET PICTURE · EXECUTION · DECISION JOURNAL), chat bottom bar, keys `/` chat · `m` mode toggle · `q` quit, minimum 96×28, same build/run instructions (`go build -o hyperagent-tui ./src`, `--core-url`, `HYPERAGENT_TOKEN`). Keep the existing sections about the daemon API dependency and the module split rationale.

- [ ] **Step 6: Build and smoke-run against the daemon (manual verification)**

```bash
cd /home/athan/projects/hypertrader/tui && go build -o hyperagent-tui ./src
```

If a daemon is running on :8787, run `./hyperagent-tui` in a real terminal and verify: panels populate, journal streams, `m` flips the mode, `/` opens chat, `q` quits. Also verify the MARKET PICTURE column scaling (FUND/OIΔ/CVD units) against known values and correct the display multipliers in `marketsView` if they read wrong — they were set from reasonable assumptions, not observed wire data. If no daemon is available, note that in the task report so the orchestrator schedules live verification.

- [ ] **Step 7: Commit**

```bash
git add -A tui/ && git commit -m "feat(tui)!: cockpit replaces the multi-view TUI — mock layout on live daemon data"
```

---

## Self-Review Notes

- Spec coverage: layout/panels (Task 3), re-scoped MANDATE (Tasks 2–3), journal mapping (Tasks 2–3), chat bar + trimmed commands (Task 4), bridge reuse + old-view deletion + README (Task 5), error handling (conn state in header, errors as journal entries — Tasks 3, 5), testing strategy (each task).
- Deviation from spec: new code lives in `tui/internal/cockpit` (new package) rather than rebuilt-in-place `tui/internal/tui` — same module, avoids name collisions during the transition, and the old package is deleted in Task 5 as specified. The spec's `/model` command is dropped from the surviving list because its picker UI was overlay-based; `/mode` covers the control need. Spec's "reply pane expands over the journal" is implemented as chatOpen swapping the journal panel for the AGENT panel.
- Type consistency check: `journalEntry{at, tag, text}`, `envelope{ExposureUSD, OpenCount, UPnL, Risk}`, `Config{Cache, Controls, Settings, ChatFn}`, message types copied verbatim from bridge.go — names match across Tasks 2–5.
