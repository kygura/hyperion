// pi harness adapter. Runs `pi -p --mode json --no-tools --provider <sub>
// --model <model>` as a subprocess with the user prompt on stdin, and parses
// pi's JSONL event stream for the final assistant message text.
//
// Verified empirically this build: pi reads the prompt from stdin (the user
// message content echoed back matched the piped stdin exactly), and exits 0 even
// on an error turn — so the stopReason of the final assistant message, not the
// process exit code, is what distinguishes success from failure.
package reasoner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PiProvider runs completions through the `pi` CLI, bound to one of pi's own
// sub-providers (e.g. "openai-codex", "anthropic").
type PiProvider struct {
	name        string
	subProvider string // pi's --provider value
	model       string
	run         runner
}

// NewPiProvider builds a pi-harness adapter. name is used for status/journaling;
// subProvider is pi's --provider flag (e.g. "openai-codex"); model is the default
// model id when a Request doesn't override it.
func NewPiProvider(name, subProvider, model string) *PiProvider {
	return &PiProvider{name: name, subProvider: subProvider, model: model, run: execRunner}
}

func (p *PiProvider) Name() string { return p.name }

// piEvent is one JSONL line from `pi -p --mode json` — only the fields we read.
type piEvent struct {
	Type    string `json:"type"`
	Message struct {
		Role         string `json:"role"`
		StopReason   string `json:"stopReason"`
		ErrorMessage string `json:"errorMessage"`
		Content      []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// Complete runs one pi completion and turns its stdout into a Response.
func (p *PiProvider) Complete(ctx context.Context, req Request) (Response, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	system, user := splitSystemUser(buildMessages(req))
	// --no-tools: pure stateless text completion, no filesystem/shell access.
	// --no-session: don't litter session files from a trading daemon.
	args := []string{"-p", "--mode", "json", "--no-tools", "--no-session",
		"--provider", p.subProvider, "--model", model, "--system-prompt", system}

	ctx, cancel := context.WithTimeout(ctx, harnessTimeout)
	defer cancel()
	out, err := p.run(ctx, "pi", args, user)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", p.name, err)
	}
	text, err := parsePiOutput(out)
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", p.name, err)
	}
	return finishResponse(req, text, p.name, model)
}

// parsePiOutput scans pi's JSONL stream and returns the final assistant message
// text. A stopReason of "error" (the real quota-exhausted shape:
// {"stopReason":"error","errorMessage":"..."}) becomes a Go error. An assistant
// message with neither text nor an error is a parse error, never a silent
// empty-string success — an empty completion on the money path could read as
// "no trade signal" instead of "we don't actually know".
func parsePiOutput(out []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), maxHarnessOutput)
	var (
		text    string
		errMsg  string
		gotAsst bool
	)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev piEvent
		if json.Unmarshal(line, &ev) != nil {
			continue // ignore non-event / unrecognized lines
		}
		if ev.Message.Role != "assistant" {
			continue
		}
		// The final assistant message is repeated across message_end / turn_end;
		// last one wins for both text and error state.
		gotAsst = true
		if ev.Message.StopReason == "error" {
			errMsg = ev.Message.ErrorMessage
			if errMsg == "" {
				errMsg = "stopReason=error with no errorMessage"
			}
		} else {
			errMsg = ""
		}
		var t strings.Builder
		for _, c := range ev.Message.Content {
			if c.Type == "text" {
				t.WriteString(c.Text)
			}
		}
		if s := t.String(); s != "" {
			text = s
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("scan output: %w", err)
	}
	if errMsg != "" {
		return "", fmt.Errorf("%s", errMsg)
	}
	if !gotAsst {
		return "", fmt.Errorf("no assistant message in output: %s", bodySnippet(out))
	}
	if text == "" {
		return "", fmt.Errorf("assistant message had no text content: %s", bodySnippet(out))
	}
	return text, nil
}
