package reasoner

import (
	"context"
	"os"
	"strings"
	"testing"
)

// kimiAgentRunner records the invocation like captureRunner, and additionally
// snapshots the --agent-file contents while the temp dir still exists — the
// empty tools allowlist is the security property under test.
func kimiAgentRunner(out string, capture *captureRunner, agentSpec *string) runner {
	return func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
		capture.bin, capture.args, capture.stdin = bin, args, stdin
		for i, a := range args {
			if a == "--agent-file" && i+1 < len(args) {
				if b, err := os.ReadFile(args[i+1]); err == nil {
					*agentSpec = string(b)
				}
			}
		}
		return []byte(out), nil
	}
}

func TestKimiComplete_Success(t *testing.T) {
	// Documented stream-json shape plus interleaved chrome/unknown lines.
	out := "TurnBegin(user_input='hi')\n" +
		`{"role":"user","content":"hi"}` + "\n" +
		`{"role":"assistant","content":"HELLO"}` + "\n" +
		"TurnEnd()\n"
	cap := &captureRunner{}
	var spec string
	k := NewKimiProvider("kimi-harness", "kimi-for-coding")
	k.run = kimiAgentRunner(out, cap, &spec)

	resp, err := k.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply != "HELLO" {
		t.Fatalf("reply = %q; want HELLO", resp.Reply)
	}
	if cap.bin != "kimi" {
		t.Errorf("bin = %q; want kimi", cap.bin)
	}
	// Print mode with machine-parseable output, and prompt rides stdin (not argv).
	for _, want := range []string{"--print", "--output-format", "stream-json", "--agent-file", "--model", "kimi-for-coding"} {
		if !hasArg(cap.args, want) {
			t.Errorf("%s not passed: %v", want, cap.args)
		}
	}
	if cap.stdin != "hi" {
		t.Errorf("prompt not on stdin: got %q", cap.stdin)
	}
	// The agent spec must carry the empty tools allowlist — kimi's print mode
	// auto-approves tool calls, so `tools: []` is the only thing standing between
	// this daemon and an unattended shell.
	if !strings.Contains(spec, "tools: []") {
		t.Errorf("agent spec missing empty tools allowlist:\n%s", spec)
	}
}

func TestKimiComplete_LastAssistantWins(t *testing.T) {
	out := `{"role":"assistant","content":"draft"}` + "\n" +
		`{"role":"assistant","content":"FINAL"}` + "\n"
	cr := &captureRunner{out: out}
	k := NewKimiProvider("kimi-harness", "")
	k.run = cr.run

	resp, err := k.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply != "FINAL" {
		t.Fatalf("reply = %q; want FINAL", resp.Reply)
	}
}

func TestKimiComplete_NoAssistant(t *testing.T) {
	// Chrome only, no assistant message → error, never an empty-string success.
	cr := &captureRunner{out: "TurnBegin(user_input='hi')\nTurnEnd()\n"}
	k := NewKimiProvider("kimi-harness", "")
	k.run = cr.run

	_, err := k.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error when output has no assistant message, got nil")
	}
}

func TestKimiComplete_RunnerError(t *testing.T) {
	k := NewKimiProvider("kimi-harness", "")
	k.run = func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}

	_, err := k.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error when runner fails, got nil")
	}
}
