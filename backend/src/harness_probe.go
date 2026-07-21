// harness_probe: shared per-harness health probes reused by `auth` (binary
// detection) and `doctor` (full health report). Every probe is READ-ONLY and
// never spends an LLM completion call — no cost, no quota risk. None of them
// echo PII: claude's auth status JSON carries the user's email/org, so only the
// loggedIn bool is extracted, never the raw object.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// cliRunner runs a harness CLI and returns its combined stdout+stderr.
// Injectable so doctor's tests can feed canned output with no real subprocess,
// mirroring the reasoner's `runner` seam in harness.go.
type cliRunner func(ctx context.Context, bin string, args ...string) (string, error)

// realCLI is the default cliRunner. It runs under the same deny-by-default
// allow-list as every other harness subprocess — a status probe never needs an
// exchange key or an API key either.
func realCLI(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = reasoner.AllowlistEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// harnessBin maps a harness name to its binary; "" for unknown.
func harnessBin(harness string) string {
	switch harness {
	case "pi", "claude", "codex", "kimi":
		return harness
	}
	return ""
}

// probeBinary resolves a harness binary on PATH.
func probeBinary(harness string) (string, error) {
	bin := harnessBin(harness)
	if bin == "" {
		return "", fmt.Errorf("unknown harness %q", harness)
	}
	return exec.LookPath(bin)
}

// authState is a compact, PII-free auth signal for doctor to render.
type authState struct {
	State  string // "ok" | "logged-out" | "unknown"
	Detail string // short human note — never raw JSON (no email/org)
}

// probeAuth reports login state per harness without echoing any PII.
//   - claude: `claude auth status --json`, extract ONLY the loggedIn bool.
//   - codex:  `codex login status`, plain text "Logged in ..." vs not.
//   - kimi:   no auth-status command exists (login is `kimi login`) → "unknown".
//   - pi:     no auth primitive exists → honestly "unknown".
func probeAuth(ctx context.Context, run cliRunner, harness string) authState {
	switch harness {
	case "claude":
		out, err := run(ctx, "claude", "auth", "status", "--json")
		if strings.TrimSpace(out) == "" {
			return authState{"unknown", "claude auth status produced no output: " + errString(err)}
		}
		var s struct {
			LoggedIn bool `json:"loggedIn"`
		}
		if e := json.Unmarshal([]byte(out), &s); e != nil {
			return authState{"unknown", "unparseable claude auth status"}
		}
		if s.LoggedIn {
			return authState{"ok", "logged in"}
		}
		return authState{"logged-out", "not logged in"}
	case "codex":
		out, err := run(ctx, "codex", "login", "status")
		if strings.Contains(out, "Logged in") {
			return authState{"ok", "logged in"}
		}
		// Don't discard the probe error: a failed subprocess (timeout, crash,
		// permission) leaves out empty — report an honest "unknown", not a
		// confidently-wrong "logged-out". Mirrors the claude branch above.
		if err != nil {
			return authState{"unknown", "codex login status failed: " + errString(err)}
		}
		return authState{"logged-out", firstLine(out)}
	case "kimi":
		return authState{"unknown", "kimi has no auth-status command; login via `hyperagent auth kimi`"}
	case "pi":
		return authState{"unknown", "pi has no auth-status command; auth via provider env/--api-key (see `pi config`)"}
	}
	return authState{"unknown", "unknown harness"}
}

// probeModels returns a cheap, honest model-availability signal. It NEVER spends
// an LLM completion (no cost/quota):
//   - pi: `pi --list-models` proves a model name is CATALOGUED locally, NOT that
//     live auth works — labeled "listed", not "reachable".
//   - claude/codex: no separate zero-cost list; their auth status already
//     implies model reachability, so doctor reads probeAuth for those instead of
//     burning a call here.
func probeModels(ctx context.Context, run cliRunner, harness string) (string, error) {
	switch harness {
	case "pi":
		out, err := run(ctx, "pi", "--list-models")
		if err != nil {
			return "", fmt.Errorf("pi --list-models: %w", err)
		}
		return fmt.Sprintf("%d models listed (catalog only, not a live-auth check)", countModelLines(out)), nil
	case "claude", "codex":
		return "covered by auth status (no zero-cost model list)", nil
	case "kimi":
		// kimi has neither an auth-status nor a proven zero-cost model list;
		// don't fabricate a signal from `kimi provider list` parsing.
		return "no zero-cost model list (kimi has no auth-status command)", nil
	}
	return "", fmt.Errorf("unknown harness %q", harness)
}

// countModelLines counts model rows in `pi --list-models` output, skipping the
// blank/header line (the header row starts with "provider").
func countModelLines(out string) int {
	n := 0
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "provider") {
			continue
		}
		n++
	}
	return n
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func errString(err error) string {
	if err == nil {
		return "no error"
	}
	return err.Error()
}
