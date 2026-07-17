package reasoner

import (
	"context"
	"strings"
	"testing"
)

// piSuccessJSONL is the assumed success shape: an assistant message_end with a
// text content block and a non-error stopReason (Anthropic-style blocks, per the
// pi event envelope observed live).
const piSuccessJSONL = `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}
{"type":"message_start","message":{"role":"assistant","content":[]}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"PONG"}],"stopReason":"end_turn"}}
{"type":"agent_settled"}`

// piErrorJSONL is the real captured quota-exhausted shape (exit code was 0).
const piErrorJSONL = `{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}
{"type":"message_end","message":{"role":"assistant","content":[],"api":"openai-codex-responses","provider":"openai-codex","model":"gpt-5.6-luna","stopReason":"error","errorMessage":"Codex error: The usage limit has been reached"}}`

func TestPiComplete_Success(t *testing.T) {
	cr := &captureRunner{out: piSuccessJSONL}
	p := NewPiProvider("pi-harness", "openai-codex", "gpt-5.6-luna")
	p.run = cr.run

	resp, err := p.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply != "PONG" {
		t.Fatalf("reply = %q; want PONG", resp.Reply)
	}
	// Tools must be disabled and the prompt must ride stdin, not argv.
	if !hasArg(cr.args, "--no-tools") {
		t.Errorf("--no-tools not passed: %v", cr.args)
	}
	if !hasArg(cr.args, "--provider") || !hasArg(cr.args, "openai-codex") {
		t.Errorf("--provider openai-codex not passed: %v", cr.args)
	}
	if cr.stdin != "hi" {
		t.Errorf("prompt not on stdin: got %q", cr.stdin)
	}
	for _, a := range cr.args {
		if a == "hi" {
			t.Errorf("prompt leaked into argv: %v", cr.args)
		}
	}
}

func TestPiComplete_ErrorStopReason(t *testing.T) {
	cr := &captureRunner{out: piErrorJSONL}
	p := NewPiProvider("pi-harness", "openai-codex", "gpt-5.6-luna")
	p.run = cr.run

	_, err := p.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error on stopReason=error, got nil")
	}
	if !strings.Contains(err.Error(), "usage limit") {
		t.Fatalf("error missing errorMessage text: %v", err)
	}
}

func TestPiComplete_RunnerError(t *testing.T) {
	p := NewPiProvider("pi-harness", "openai-codex", "gpt-5.6-luna")
	p.run = func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}

	_, err := p.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error when runner fails, got nil")
	}
}

func TestPiComplete_Malformed(t *testing.T) {
	// No assistant message anywhere → clear parse error, never empty success.
	cr := &captureRunner{out: `{"type":"agent_start"}` + "\ngarbage not json\n"}
	p := NewPiProvider("pi-harness", "openai-codex", "gpt-5.6-luna")
	p.run = cr.run

	_, err := p.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error on malformed output, got nil")
	}
}
