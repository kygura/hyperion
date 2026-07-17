package reasoner

import (
	"context"
	"os"
	"strings"
	"testing"
)

// codexFileRunner writes want to the --output-last-message path the adapter
// passed, mimicking codex dumping its final message to the tmpfile.
func codexFileRunner(want string, capture *captureRunner) runner {
	return func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
		capture.bin, capture.args, capture.stdin = bin, args, stdin
		for i, a := range args {
			if a == "--output-last-message" && i+1 < len(args) {
				_ = os.WriteFile(args[i+1], []byte(want), 0o600)
			}
		}
		return nil, nil
	}
}

func TestCodexComplete_Success(t *testing.T) {
	cap := &captureRunner{}
	c := NewCodexProvider("codex-harness", "gpt-5.6-luna")
	c.run = codexFileRunner("CODEX REPLY", cap)

	resp, err := c.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply != "CODEX REPLY" {
		t.Fatalf("reply = %q; want CODEX REPLY", resp.Reply)
	}
	// read-only sandbox must be requested, and prompt rides stdin (not argv).
	if !hasArg(cap.args, "--sandbox") || !hasArg(cap.args, "read-only") {
		t.Errorf("--sandbox read-only not passed: %v", cap.args)
	}
	if !strings.Contains(cap.stdin, "hi") {
		t.Errorf("prompt not on stdin: got %q", cap.stdin)
	}
}

func TestCodexComplete_EmptyFile(t *testing.T) {
	cap := &captureRunner{}
	c := NewCodexProvider("codex-harness", "gpt-5.6-luna")
	// Runner succeeds but writes nothing → empty last-message file is an error.
	c.run = func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
		return nil, nil
	}

	_, err := c.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error on empty last-message file, got nil")
	}
	_ = cap
}

func TestCodexComplete_RunnerError(t *testing.T) {
	c := NewCodexProvider("codex-harness", "gpt-5.6-luna")
	c.run = func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}

	_, err := c.Complete(context.Background(), Request{Role: RoleChat, UserMessage: "hi"})
	if err == nil {
		t.Fatal("want error when runner fails, got nil")
	}
}
