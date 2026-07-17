# SPEC — Harness-backed SOTA reasoning for the Hyperion trading loop

## Motivation

The trading daemon (`backend/`) currently reasons over market data through two
direct HTTP API providers, OpenAI and Deepseek (`backend/internal/reasoner/openai.go`,
`config.toml` defaults `batch_provider = "deepseek"`, `chat_provider = "deepseek"`).
These provide little value for the quality of trading decisions the system needs.

This change adds a new class of `Provider` implementation that runs state-of-the-art
models by spawning the user's already-authenticated CLI harnesses — `pi`, `claude`,
and (structurally) `codex` — as subprocesses, instead of calling a raw API with a
stored key. It also fixes a latent bug that collapses thesis formation and trade
execution decisions onto one provider binding, and makes harness-backed reasoning
the default for both of those roles.

Out of scope: `dashboard/` (unrelated, per explicit instruction), any new dry-run/
paper-trading product feature (none requested; existing `execution.mode` is
untouched), any TUI screen changes (none needed — `tui/` talks to `backend/` over
HTTP+WS only and this change doesn't touch the API's wire shape beyond exposing new
provider names, which the existing settings picker already renders generically).

## Current architecture (verified by exploration, see below)

- `Provider` interface (`backend/internal/reasoner/provider.go:93`): two methods,
  `Name() string` and `Complete(ctx, Request) (Response, error)`. `openai.go` and
  `anthropic.go` are the only adapters. Both share `buildMessages(req)` (system+user
  message construction) and `finishResponse(req, text, provider, model)` (parses the
  model's raw text into `Response{Verdicts, Reviews, Reply}` depending on `req.Role`).
- `Role` has four values: `RoleBatch`, `RoleChat`, `RoleReview` (thesis formation —
  produces `ThesisReview` ops), `RoleTrigger` (gate-fired deviation check — produces
  trade `Verdict`s). Different prompts already exist per role
  (`BuildReviewPrompt`/`BuildTriggerPrompt`/`BuildBatchPrompt`).
- **Bug**: `Registry` (`engine.go`) binds `(provider, model)` per `Role`, and the
  binding is already runtime-switchable through the existing HTTP settings endpoint
  (`backend/internal/api/settings.go`, `handlePutSettings`) — no new code needed for
  manual provider switching. But `NewRegistry` only seeds bindings for `RoleBatch`
  and `RoleChat`, and `Engine.reason()` hardcodes `e.registry.For(RoleBatch)`
  regardless of the actual role being processed. Thesis formation (`RoleReview`) and
  trade-decision (`RoleTrigger`) therefore always ride the *same* provider/model
  today even though their prompts differ — this is the concrete form of "OpenAI/
  Deepseek provide little value here": one model, one binding, doing two
  structurally different jobs.
- Config (`backend/internal/config/config.go`): `Providers` struct has named
  `Anthropic`/`OpenAI`/`Deepseek` fields plus a `Custom map[string]ProviderCfg` for
  arbitrary endpoints. `ProviderCfg.Kind` selects wire protocol (`"openai"` default |
  `"anthropic"`). `buildReasoner` (`backend/src/main.go:351`) always registers
  anthropic/openai/deepseek in the `Registry`, even with no key configured.
- No `os/exec` usage anywhere in `backend/` today — no subprocess precedent.
- Test convention: no mocking framework, hand-rolled fakes implementing `Provider`
  directly (`recordProvider`, `scriptProvider`, `roleProvider`, `fakeChatProvider`).

## New architecture

### Harness providers

Three new adapter files in `backend/internal/reasoner/`, one per CLI, sharing one
small subprocess-spawn helper (`harness.go`). Each adapter implements `Provider` and
reuses the existing `buildMessages`/`finishResponse` helpers exactly like `openai.go`
does — the only new logic is turning a `Request` into a subprocess invocation and
turning that subprocess's stdout back into plain text.

Rationale for three files instead of one generic switch-on-CLI-name provider: the
three CLIs have incompatible output protocols (pi: streaming JSONL events; claude:
one JSON object with a `.result` field; codex: JSONL events or a dumped
last-message file) and only one of them (`claude`) could be verified end-to-end
during this build (`pi`'s `openai-codex` backend and `codex` both returned "usage
limit reached" when tested live — no quota this session). Separate files let each
adapter be unit-tested in isolation against its own protocol via an injected runner
function, so the one verified path doesn't share a test surface with the two
structurally-wired-but-unverified ones.

- `harness.go`: a small injectable subprocess runner type (e.g.
  `type runner func(ctx context.Context, bin string, args []string, stdin string) (stdout []byte, err error)`),
  with a real `os/exec`-backed default implementation. Real implementation must:
  pass the prompt via **stdin**, not argv (ARG_MAX + shell-injection risk with
  market-data text); enforce a `context.WithTimeout` (mirror the existing 90–120s
  pattern in `engine.go`/`anthropic.go`); run the CLI with **all tools/file-edit
  capability disabled** (this is a subprocess spawned by a trading daemon — it must
  behave as a stateless text-completion call, never touch the filesystem or shell);
  never log/print the constructed command if it could contain secrets (none should:
  these paths carry no API keys, that's the point of using the CLI's own auth).
- `pi.go`: `PiProvider{name, subProvider, model string}`. Invokes
  `pi -p --mode json --no-tools --provider <subProvider> --model <model> --system-prompt <system>`
  with the user prompt on stdin (or via message arg — implementer verifies exact
  stdin-vs-arg behavior against the real `pi --help` before finalizing). Parses the
  JSONL event stream for the final assistant message text; a `stopReason` of
  `"error"` (confirmed real shape: `{"stopReason":"error","errorMessage":"..."}`)
  must surface as a normal Go `error` from `Complete`, not a fabricated empty
  success — this is exactly the quota-exhausted case observed live.
- `claude.go`: `ClaudeProvider{name, model string}`. Invokes
  `claude -p --output-format json` with system prompt via `--system-prompt` and user
  prompt via stdin, all tools denied (implementer must verify and prove — via a
  cheap, tool-triggering test prompt — that the chosen flag combination actually
  blocks Bash/Edit/Write before this is wired as a default; do not assume). Parses
  the single JSON object's `.result` (text) and `.is_error` (bool) — confirmed shape
  from a live test call.
- `codex.go`: `CodexProvider{name, model string}`. Invokes
  `codex exec --sandbox read-only --json --output-last-message <tmpfile>` with the
  prompt on stdin, reads the tmpfile for the reply text. Structurally present and
  unit-tested against fakes, but **not** placed in any default role binding this
  build (no live verification possible — no quota). Available for manual selection
  via the existing settings endpoint once quota returns.

### Role-binding fix

- `NewRegistry` seeds bindings for all four roles (`RoleBatch`, `RoleChat`,
  `RoleReview`, `RoleTrigger`), reading `Reasoner.ReviewProvider`/`ReviewModel` and
  `Reasoner.TriggerProvider`/`TriggerModel` (new config fields) in addition to the
  existing batch/chat fields.
- `Engine.reason()` resolves the binding for the **actual** role being processed
  (`e.registry.For(role)`) instead of hardcoding `RoleBatch`. `RoleBatch` (the
  legacy/fallback digest kind) keeps its own binding, defaulting to the same
  provider/model as `RoleTrigger` unless configured otherwise.
- This is the concrete "delegates tasks between thesis formation and trade
  execution policy" the user asked for: two independently-bound roles, each free to
  run a different model via a different transport.

### Default provider assignment (this build)

- `RoleReview` (thesis formation, lower call frequency, benefits from deeper
  reasoning) → `claude` harness. This is the one path verified end-to-end this
  session.
- `RoleTrigger` and `RoleBatch` (execution policy / trade-decision, higher call
  frequency) → `pi` harness bound to `gpt-5.6-luna` via the `openai-codex`
  sub-provider — matches the user's explicit ask ("GPT-5.6 models, cheap and
  cost-efficient, make them the default") for the highest-frequency, most
  cost-sensitive role. **Caveat, stated plainly**: this path could not be exercised
  against a live GPT-5.6 response this session (no quota — confirmed by a live test
  that returned `"Codex error: The usage limit has been reached"`); the parser is
  built and unit-tested against the exact JSONL envelope shape observed in that
  live (error-path) call, defensively coded to return a clear error rather than a
  false-success on any unrecognized shape. If it misparses a real success response
  the failure mode is a journaled error on that call (existing behavior: a failed
  provider call is journaled once and the prior thesis/verdict state is
  preserved — `engine.go` has no retry/auto-trade-on-garbage path), not a bad trade
  — and the user can flip `RoleTrigger`/`RoleBatch` back to `deepseek` in one
  settings-endpoint call with zero new code, because `deepseek`/`openai`/`anthropic`
  stay registered exactly as they are today, just no longer the default.
- `RoleChat` is untouched (stays on its existing default) — it's the human-facing
  TUI chat pane, not part of the data-consumption → decision → execution loop this
  change targets, and touching it is unnecessary diff.
- `config.toml` reasoner defaults become:
  ```
  review_provider = "claude-harness"   (new)
  review_model    = ""                 (adapter's own default)
  trigger_provider = "pi-harness"      (new)
  trigger_model    = "gpt-5.6-luna"
  batch_provider   = "pi-harness"
  batch_model      = "gpt-5.6-luna"
  chat_provider = "deepseek"           (unchanged)
  chat_model    = "deepseek-chat"      (unchanged)
  ```
- `buildReasoner` in `main.go` declares the three harness providers through
  `Providers.Custom` (the same mechanism any custom/OpenAI-compatible endpoint
  already uses), not as hardcoded fields like anthropic/openai/deepseek —
  `config.toml` ships with `[providers.custom.claude-harness]`/`[providers.custom.pi-harness]`
  stanzas by default, so the harness providers exist out of the box without
  code changes if a user's `config.toml` is left at its shipped defaults. If
  the target CLI binary isn't found on `PATH`, the provider is still
  registered but every `Complete` call returns a clear "binary not found"
  error — same "fail loud on the call, not at startup" posture already used
  for missing API keys.

## Explicit non-goals / safety rails carried through this build

- No automatic failover logic between harness and direct-API providers — the
  existing runtime provider-switch endpoint is the fallback mechanism, by design
  (smallest sane addition).
- No new dry-run/paper-trading feature. Verification for this change is `go build`
  + `go test` only; the real daemon is never run against a live exchange during
  this build.
- `backend/.env` contents are never read into logs, printed, or committed. Harness
  paths carry no API keys by construction (that's the point — CLI's own login).
- No force-push, no remote push, conventional commits only, no AI attribution.

## Done when

- `cd backend && go build ./... && go test ./...` is green.
- `cd tui && go build ./... && go test ./...` is green (unaffected by this change,
  confirms no regression).
- `pi.go`, `claude.go`, `codex.go`, `harness.go` each have a unit test exercising at
  least: one success-shape parse, one error-shape parse (the real
  `"stopReason":"error"` / `.is_error` cases), using an injected fake runner — no
  real subprocess spawned in the automated suite.
- `Engine.reason()` has a test proving `RoleReview` and `RoleTrigger` can be bound
  to two different fake providers and each is actually invoked for its own role
  (regression test for the hardcoded-`RoleBatch` bug).
- `config.toml` reflects the new defaults above; `config.go` loads them into the
  `Reasoner` struct without a schema-validation error.
- `review-risk` and `review-reliability` findings against the diff are addressed
  (subprocess spawning is the security-relevant surface of this change).
