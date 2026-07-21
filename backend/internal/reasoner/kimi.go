// kimi harness adapter. Runs `kimi --print --output-format stream-json
// --agent-file <tmpdir>/agent.yaml` as a subprocess with the user prompt on
// stdin, and parses the JSONL stream for the final assistant message text.
//
// Tool-blocking (the hard security fact this adapter depends on): kimi's print
// mode auto-approves every tool call ("Print mode ... auto-approves tool calls
// for this invocation", `kimi --help` v1.48.0) and has no --no-tools flag, so a
// default-agent print run could edit files and run commands unattended. The
// mitigation is a throwaway agent spec file: its `tools:` field is an ALLOWLIST
// (per the official agents doc), and an empty list leaves the model no tool to
// call. Verified against kimi 1.48.0 this build:
//   - an agent file naming an unknown tool is rejected loudly ("Invalid tools:
//     [...]"), proving the list is parsed and enforced at load time;
//   - `tools: []` with a system_prompt_path (empty file OK) passes agent load;
//   - system_prompt_path is required ("System prompt path is required"), which
//     is convenient: kimi has no --system-prompt flag, so the system prompt
//     travels via that file — same per-call tmpfile posture as codex.go.
//
// A live completion could NOT be verified on this machine (no kimi login); the
// stream-json assistant line shape {"role":"assistant","content":"..."} and
// stdin piping (`echo "..." | kimi --print`) come from the official print-mode
// docs. Text output mode was observed to carry UI chrome (TurnBegin/TurnEnd
// lines), which is why stream-json is load-bearing here.
package reasoner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// kimiAgentSpec is the per-call agent spec: no tools, system prompt from the
// sibling system.md. Empty `tools:` is the tool kill-switch (see file header).
const kimiAgentSpec = `version: 1
agent:
  name: hyperion-completion
  system_prompt_path: ./system.md
  tools: []
`

// KimiProvider runs completions through the `kimi` CLI.
type KimiProvider struct {
	name  string
	model string
	run   runner
}

// NewKimiProvider builds a kimi-harness adapter. name is used for
// status/journaling; model is the default model id when a Request doesn't
// override it (empty lets the CLI pick its own default).
func NewKimiProvider(name, model string) *KimiProvider {
	return &KimiProvider{name: name, model: model, run: execRunner}
}

func (k *KimiProvider) Name() string { return k.name }

// kimiMessage is one JSONL line from `kimi --print --output-format stream-json`
// — only the fields we read. Documented shape:
// {"role":"assistant","content":"<text>"}.
type kimiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Complete runs one kimi completion and turns its stdout into a Response.
func (k *KimiProvider) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = k.model
	}
	system, user := splitSystemUser(buildMessages(req))

	// Throwaway agent dir: agent.yaml (constant, no tools) + system.md (per-call
	// system prompt; an empty file is accepted).
	dir, err := os.MkdirTemp("", "kimi-agent-*")
	if err != nil {
		return Response{}, fmt.Errorf("%s: temp dir: %w", k.name, err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(system), 0o600); err != nil {
		return Response{}, fmt.Errorf("%s: write system prompt: %w", k.name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(kimiAgentSpec), 0o600); err != nil {
		return Response{}, fmt.Errorf("%s: write agent spec: %w", k.name, err)
	}

	// --agent-file with an empty tools list disables all tools (see file header).
	args := []string{"--print", "--output-format", "stream-json",
		"--agent-file", filepath.Join(dir, "agent.yaml")}
	if model != "" {
		args = append(args, "--model", model)
	}

	ctx, cancel := context.WithTimeout(ctx, harnessTimeout)
	defer cancel()
	out, err := k.run(ctx, "kimi", args, user)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", k.name, err)
	}
	text, err := parseKimiOutput(out)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", k.name, err)
	}
	return finishResponse(req, text, k.name, model)
}

// parseKimiOutput scans kimi's JSONL stream and returns the last assistant
// message text. Hard failures (auth, quota, "LLM not set") exit nonzero and are
// caught by the runner before this parse; here an output with no non-empty
// assistant message is an error, never a silent empty-string success on the
// money path.
func parseKimiOutput(out []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), maxHarnessOutput)
	var text string
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var m kimiMessage
		if json.Unmarshal(line, &m) != nil {
			continue // ignore non-message / unrecognized lines
		}
		// Assistant messages stream in order (tool-call turns would interleave,
		// but this agent has no tools); the last non-empty one is the reply.
		if m.Role == "assistant" && m.Content != "" {
			text = m.Content
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("scan output: %w", err)
	}
	if text == "" {
		return "", fmt.Errorf("no assistant message in output: %s", bodySnippet(out))
	}
	return text, nil
}
