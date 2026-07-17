package reasoner

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// captureRunner is a fake runner that records the invocation and returns a
// canned stdout/err. Adapters set p.run = cr.run to unit-test parse logic and
// flag construction without spawning a real subprocess.
type captureRunner struct {
	out   string
	err   error
	bin   string
	args  []string
	stdin string
}

func (c *captureRunner) run(_ context.Context, bin string, args []string, stdin string) ([]byte, error) {
	c.bin = bin
	c.args = args
	c.stdin = stdin
	return []byte(c.out), c.err
}

// hasArg reports whether flag appears in args.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func TestLimitedWriterCaps(t *testing.T) {
	var buf bytes.Buffer
	lw := &limitedWriter{w: &buf, n: 5}
	// Report full length written so os/exec sees no short write, but only n land.
	if n, err := lw.Write([]byte("0123456789")); err != nil || n != 10 {
		t.Fatalf("Write = %d, %v; want 10, nil", n, err)
	}
	if got := buf.String(); got != "01234" {
		t.Fatalf("capped buffer = %q; want %q", got, "01234")
	}
	if n, _ := lw.Write([]byte("more")); n != 4 {
		t.Fatalf("post-cap Write = %d; want 4 (reported consumed)", n)
	}
	if buf.Len() != 5 {
		t.Fatalf("buffer grew past cap: len=%d", buf.Len())
	}
}

func TestSplitSystemUser(t *testing.T) {
	msgs := []oaiMessage{
		{Role: "system", Content: "SYS"},
		{Role: "user", Content: "hello"},
	}
	sys, user := splitSystemUser(msgs)
	if sys != "SYS" || user != "hello" {
		t.Fatalf("splitSystemUser = %q,%q; want SYS,hello", sys, user)
	}
	// History folds into the stdin block with role labels.
	msgs = append(msgs, oaiMessage{Role: "assistant", Content: "hi back"}, oaiMessage{Role: "user", Content: "again"})
	_, user = splitSystemUser(msgs)
	if !strings.Contains(user, "hello") || !strings.Contains(user, "assistant: hi back") || !strings.Contains(user, "again") {
		t.Fatalf("folded user block missing turns: %q", user)
	}
}
