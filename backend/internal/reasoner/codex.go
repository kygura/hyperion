// codex harness adapter. Runs `codex exec --sandbox read-only --json
// --output-last-message <tmpfile>` as a subprocess with the prompt on stdin, and
// reads the tmpfile for the reply text.
//
// Verified against `codex exec --help` this build: exec reads instructions from
// stdin when no positional prompt is given; --sandbox read-only,
// --output-last-message, and --json all exist. codex exec has no --system-prompt
// flag, so the system framing is prepended to the prompt.
//
// SECURITY — read this before treating codex as safe: `--sandbox read-only` does
// NOT give the same containment as pi's --no-tools or claude's --tools "". It only
// blocks WRITE access for auto-approved commands; codex's shell/read tools remain
// reachable, so the model can still run read-only commands (printenv, file reads,
// env dumps) inside the sandbox. There is no flag in this codex build that fully
// disables its tool/shell use (no --no-tools; --disable toggles feature flags, not
// the core shell). The actual mitigation is therefore twofold: (a) execRunner in
// harness.go hands the subprocess a minimal allow-listed env with NO exchange key
// or API key to read, and (b) codex is never wired into a default role binding —
// it is structural/opt-in only, selected manually via the settings endpoint (per
// SPEC.md). Do not place codex in a default binding without revisiting this.
package reasoner

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// CodexProvider runs completions through the `codex` CLI.
type CodexProvider struct {
	name  string
	model string
	run   runner
}

// NewCodexProvider builds a codex-harness adapter. name is used for
// status/journaling; model is the default model id when a Request doesn't
// override it (empty lets the CLI pick its own default).
func NewCodexProvider(name, model string) *CodexProvider {
	return &CodexProvider{name: name, model: model, run: execRunner}
}

func (c *CodexProvider) Name() string { return c.name }

// Complete runs one codex completion and turns the last-message tmpfile into a
// Response.
func (c *CodexProvider) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}
	system, user := splitSystemUser(buildMessages(req))
	prompt := user
	if system != "" {
		prompt = system + "\n\n" + user
	}

	f, err := os.CreateTemp("", "codex-last-*.txt")
	if err != nil {
		return Response{}, fmt.Errorf("%s: temp file: %w", c.name, err)
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)

	// --sandbox read-only blocks writes only; codex's read/shell tools stay
	// reachable (see the security note in the file header). Safe here because the
	// subprocess env carries no secrets and codex is opt-in, never a default role.
	args := []string{"exec", "--sandbox", "read-only", "--json", "--output-last-message", tmp}
	if model != "" {
		args = append(args, "--model", model)
	}

	ctx, cancel := context.WithTimeout(ctx, harnessTimeout)
	defer cancel()
	if _, err := c.run(ctx, "codex", args, prompt); err != nil {
		return Response{}, fmt.Errorf("%s: %w", c.name, err)
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		return Response{}, fmt.Errorf("%s: read last-message file: %w", c.name, err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		// Empty/missing last message is a failure, not an empty success.
		return Response{}, fmt.Errorf("%s: empty last-message file", c.name)
	}
	return finishResponse(req, text, c.name, model)
}
