// claude harness adapter. Runs `claude -p --output-format json --tools ""` as a
// subprocess with the user prompt on stdin, and parses the single result JSON
// object's .result / .is_error fields.
//
// Tool-blocking, verified empirically this build (the one hard security fact
// this adapter depends on):
//   - `claude --help` documents `--tools ""` as "disable all tools".
//   - Proof it takes effect: piped "read marker.txt and tell me its contents,
//     use your file tools" with `--tools ""` returned num_turns=1 and no file
//     contents — the model had no tool to call, so it only claimed intent.
//   - Positive control: the same prompt with tools enabled
//     (`--permission-mode bypassPermissions`) returned num_turns=2 and the
//     actual file contents. So `--tools ""` genuinely strips the tools, it isn't
//     the model merely declining.
//
// This runs inside a trading daemon and must behave as pure stateless text
// completion — `--tools ""` is load-bearing, do not remove it.
package reasoner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// ClaudeProvider runs completions through the `claude` CLI.
type ClaudeProvider struct {
	name  string
	model string
	run   runner
}

// NewClaudeProvider builds a claude-harness adapter. name is used for
// status/journaling; model is the default model id when a Request doesn't
// override it (empty lets the CLI pick its own default).
func NewClaudeProvider(name, model string) *ClaudeProvider {
	return &ClaudeProvider{name: name, model: model, run: execRunner}
}

func (c *ClaudeProvider) Name() string { return c.name }

// claudeResult is the single JSON object from `claude -p --output-format json` —
// only the fields we read. Real captured shape:
// {"type":"result","subtype":"success","is_error":false,...,"result":"<text>"}.
type claudeResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// Complete runs one claude completion and turns its stdout into a Response.
func (c *ClaudeProvider) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}
	system, user := splitSystemUser(buildMessages(req))
	// --tools "" disables all tools (empirically verified above).
	args := []string{"-p", "--output-format", "json", "--tools", ""}
	if system != "" {
		args = append(args, "--system-prompt", system)
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	ctx, cancel := context.WithTimeout(ctx, harnessTimeout)
	defer cancel()
	out, err := c.run(ctx, "claude", args, user)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", c.name, err)
	}
	text, err := parseClaudeOutput(out)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", c.name, err)
	}
	return finishResponse(req, text, c.name, model)
}

// parseClaudeOutput reads the single result object. is_error:true becomes a Go
// error carrying whatever error text the object holds; otherwise .result is the
// reply. An empty .result or an unrecognized shape is an error, never a silent
// empty-string success on the money path.
func parseClaudeOutput(out []byte) (string, error) {
	var r claudeResult
	if err := json.Unmarshal(bytes.TrimSpace(out), &r); err != nil {
		return "", fmt.Errorf("unrecognized output: %s", bodySnippet(out))
	}
	if r.IsError {
		msg := r.Result
		if msg == "" {
			msg = r.Subtype
		}
		if msg == "" {
			msg = "unknown error"
		}
		return "", fmt.Errorf("%s", msg)
	}
	if r.Result == "" {
		return "", fmt.Errorf("empty result: %s", bodySnippet(out))
	}
	return r.Result, nil
}
