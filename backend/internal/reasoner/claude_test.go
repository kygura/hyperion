package reasoner

import (
	"context"
	"strings"
	"testing"
)

func TestClaudeComplete_Success(t *testing.T) {
	// Real captured success shape.
	cr := &captureRunner{out: `{"type":"result","subtype":"success","is_error":false,"result":"HELLO"}`}
	c := NewClaudeProvider("claude-harness", "")
	c.run = cr.run

	resp, err := c.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply != "HELLO" {
		t.Fatalf("reply = %q; want HELLO", resp.Reply)
	}
	// --tools "" (disable all tools) must be present, and prompt rides stdin.
	if !hasArg(cr.args, "--tools") {
		t.Errorf("--tools not passed: %v", cr.args)
	}
	if cr.stdin != "hi" {
		t.Errorf("prompt not on stdin: got %q", cr.stdin)
	}
}

func TestClaudeComplete_IsError(t *testing.T) {
	cr := &captureRunner{out: `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom happened"}`}
	c := NewClaudeProvider("claude-harness", "")
	c.run = cr.run

	_, err := c.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error on is_error:true, got nil")
	}
	if !strings.Contains(err.Error(), "boom happened") {
		t.Fatalf("error missing result text: %v", err)
	}
}

func TestClaudeComplete_RunnerError(t *testing.T) {
	c := NewClaudeProvider("claude-harness", "")
	c.run = func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}

	_, err := c.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error when runner fails, got nil")
	}
}

func TestClaudeComplete_Malformed(t *testing.T) {
	cr := &captureRunner{out: "not json at all"}
	c := NewClaudeProvider("claude-harness", "")
	c.run = cr.run

	_, err := c.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error on unrecognized output, got nil")
	}
}
