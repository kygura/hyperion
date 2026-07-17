// auth: in-app trigger for each reasoning harness CLI's real interactive login,
// so a login can be started from Hyperion instead of a separate shell command.
//
//	hyperagent auth <pi|claude|codex>
//
// The login subprocess inherits a REAL terminal (so OAuth/browser prompts work)
// but NOT the daemon's full env: it runs under reasoner.AllowlistEnv — the exact
// same deny-by-default filter the reasoning-call path uses (harness.go). That
// keeps HOME/XDG/config-home so the CLI can persist its own credentials, while
// every secret-shaped var (HL_AGENT_KEY, HL_MASTER_KEY, *_API_KEY, ...) is
// dropped by construction. One shared allow-list, not a hand-maintained deny-
// list that silently leaks any secret nobody remembered to add to it.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// authLoginTimeout bounds the interactive login subprocess. It is INTERACTIVE
// (a human doing an OAuth/browser sign-in), so it must be generous enough not to
// kill a real person mid-flow — 10 minutes. But it is still bounded so that a
// TTY-less invocation (script, supervisor, CI) can't hang the process forever.
// Belt-and-suspenders, mirroring harness.go's own (much shorter) exec cap.
const authLoginTimeout = 10 * time.Minute

// loginCommands maps a harness to the exact argv of its real login surface.
//   - claude: `claude auth login` — interactive Anthropic sign-in.
//   - codex:  `codex login` (bare) — interactive ChatGPT/OAuth sign-in.
//   - pi:     `pi config` — pi has NO login/auth subcommand (verified via
//     `pi --help`: only install/remove/update/list/config). Credentials are
//     resolved from provider env vars or `--api-key`; `pi config` is its only
//     interactive config TUI. Not a fake login: runAuth prints the real
//     mechanism before launching it.
var loginCommands = map[string][]string{
	"claude": {"claude", "auth", "login"},
	"codex":  {"codex", "login"},
	"pi":     {"pi", "config"},
}

const piAuthNote = `pi has no interactive login command. It authenticates from provider env vars
(e.g. ANTHROPIC_OAUTH_TOKEN, OPENAI_API_KEY) or a --api-key flag. Opening
` + "`pi config`" + ` (pi's settings/resource TUI) — set provider credentials via your
environment or .env, not here.`

// runAuth wires the real subprocess and I/O; the decision logic lives in
// buildAuth so it stays testable without spawning a login.
func runAuth(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), authLoginTimeout)
	defer cancel()
	cmd, err := buildAuth(ctx, os.Stdout, exec.LookPath, args)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// buildAuth validates the harness arg, prints any harness-specific note to w,
// and returns the ready-to-run login *exec.Cmd — WITHOUT running it. lookPath is
// injected so a test can assert the pi-note-gets-printed contract and the
// binary-not-found branch without depending on what's installed. Mirrors
// doctor.go's writeDoctor(w, ...) split (render/decide vs. spawn).
func buildAuth(ctx context.Context, w io.Writer, lookPath func(string) (string, error), args []string) (*exec.Cmd, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: hyperagent auth <pi|claude|codex>")
	}
	harness := strings.ToLower(strings.TrimSpace(args[0]))
	argv, ok := loginCommands[harness]
	if !ok {
		return nil, fmt.Errorf("unknown harness %q: use one of pi, claude, codex", harness)
	}
	if _, err := lookPath(argv[0]); err != nil {
		return nil, fmt.Errorf("%s: binary not found on PATH: %w", argv[0], err)
	}
	// pi has no login: explain the real mechanism instead of faking one.
	if harness == "pi" {
		fmt.Fprintln(w, piAuthNote)
	}
	return loginCmd(ctx, harness), nil
}

// loginCmd builds the login subprocess with an inherited TTY, a deadline, and
// the shared allow-list env. Split from buildAuth so a unit test can assert
// Args/Env/Stdin wiring WITHOUT executing a real interactive login. Caller must
// pass a harness key present in loginCommands.
func loginCmd(ctx context.Context, harness string) *exec.Cmd {
	argv := loginCommands[harness]
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// Inherited TTY: browser/OAuth flows and interactive prompts need a real
	// terminal, not the piped stdin/stdout the reasoner's completion call uses.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Deny-by-default env: same filter as the reasoning path — HOME/config-home
	// survive so the login persists, every secret-shaped var is dropped.
	cmd.Env = reasoner.AllowlistEnv()
	return cmd
}
