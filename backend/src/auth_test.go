package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hyperagent/hyperagent/internal/reasoner"
)

// okLook is an exec.LookPath stand-in that reports every binary present, so
// buildAuth tests don't depend on what's actually installed.
func okLook(name string) (string, error) { return "/usr/bin/" + name, nil }

// Command construction is verified WITHOUT running a real interactive login:
// build the *exec.Cmd and assert its wiring. Never calls .Run().
func TestLoginCmdConstruction(t *testing.T) {
	cases := map[string][]string{
		"claude": {"claude", "auth", "login"},
		"codex":  {"codex", "login"},
		"pi":     {"pi", "config"},
	}
	for harness, wantArgs := range cases {
		cmd := loginCmd(context.Background(), harness)
		if !slices.Equal(cmd.Args, wantArgs) {
			t.Errorf("%s: args = %v, want %v", harness, cmd.Args, wantArgs)
		}
		if cmd.Stdin != os.Stdin || cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
			t.Errorf("%s: TTY not inherited (stdin/out/err must be os.Std*)", harness)
		}
		if cmd.Env == nil {
			t.Errorf("%s: env must be the allow-list, not nil (nil = inherit full env incl. keys)", harness)
		}
	}
}

func TestRunAuthUnknownAndMissingArg(t *testing.T) {
	if err := runAuth(nil); err == nil {
		t.Error("missing arg: want usage error, got nil")
	}
	if err := runAuth([]string{"bogus"}); err == nil {
		t.Error("unknown harness: want error, got nil")
	}
}

// buildAuth prints pi's real-mechanism note (the "explains itself instead of
// faking a login" contract) without spawning a subprocess.
func TestBuildAuthPiNote(t *testing.T) {
	var buf bytes.Buffer
	cmd, err := buildAuth(context.Background(), &buf, okLook, []string{"pi"})
	if err != nil {
		t.Fatalf("pi: unexpected err %v", err)
	}
	if cmd == nil {
		t.Fatal("pi: want a cmd to run")
	}
	if !strings.Contains(buf.String(), "pi has no interactive login command") {
		t.Errorf("pi note not printed; got %q", buf.String())
	}

	buf.Reset()
	if _, err := buildAuth(context.Background(), &buf, okLook, []string{"claude"}); err != nil {
		t.Fatalf("claude: unexpected err %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("claude must print no pi note; got %q", buf.String())
	}
}

// buildAuth surfaces the binary-not-found branch (exec.LookPath failure).
func TestBuildAuthBinaryNotFound(t *testing.T) {
	fail := func(string) (string, error) { return "", errors.New("not found") }
	if _, err := buildAuth(context.Background(), &bytes.Buffer{}, fail, []string{"claude"}); err == nil {
		t.Error("binary missing: want error, got nil")
	}
}

func TestAllowlistEnvStripsEverySecret(t *testing.T) {
	t.Setenv("HL_AGENT_KEY", "0xdeadbeef")
	t.Setenv("HL_MASTER_KEY", "0xmasterpriv") // the bug: was leaking before
	t.Setenv("HL_MASTER_ADDRESS", "0xmaster")
	t.Setenv("HL_AGENT_ADDRESS", "0xagent")
	t.Setenv("OPENAI_API_KEY", "sk-leak")
	t.Setenv("DEEPSEEK_API_KEY", "sk-leak2")
	t.Setenv("HOME", "/home/tester")

	env := reasoner.AllowlistEnv()
	has := func(name string) bool {
		for _, kv := range env {
			if n, _, _ := strings.Cut(kv, "="); n == name {
				return true
			}
		}
		return false
	}
	for _, k := range []string{
		"HL_AGENT_KEY", "HL_MASTER_KEY", "HL_MASTER_ADDRESS",
		"HL_AGENT_ADDRESS", "OPENAI_API_KEY", "DEEPSEEK_API_KEY",
	} {
		if has(k) {
			t.Errorf("%s must be stripped from every harness subprocess env", k)
		}
	}
	if !has("HOME") {
		t.Error("HOME must pass through (login writes credentials under it)")
	}
}

func fakeCLI(byArgs map[string]string) cliRunner {
	return func(_ context.Context, bin string, args ...string) (string, error) {
		return byArgs[strings.Join(append([]string{bin}, args...), " ")], nil
	}
}

// fakeCLIError models a probe subprocess that itself fails (timeout/crash), so
// out is empty and err is set — the case codex used to swallow.
func fakeCLIError(err error) cliRunner {
	return func(_ context.Context, _ string, _ ...string) (string, error) {
		return "", err
	}
}

// A failing probe subprocess must yield an honest "unknown", never a
// confidently-wrong "logged-out", for both claude and codex.
func TestProbeAuthSubprocessError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("probe crashed")
	for _, h := range []string{"claude", "codex"} {
		if s := probeAuth(ctx, fakeCLIError(boom), h); s.State != "unknown" {
			t.Errorf("%s probe error: state = %q, want unknown (detail %q)", h, s.State, s.Detail)
		}
	}
}

func TestProbeAuth(t *testing.T) {
	ctx := context.Background()

	in := probeAuth(ctx, fakeCLI(map[string]string{
		"claude auth status --json": `{"loggedIn":true,"email":"x@y.z","orgName":"acme"}`,
	}), "claude")
	if in.State != "ok" {
		t.Errorf("claude logged-in: state = %q, want ok", in.State)
	}
	if strings.Contains(in.Detail, "x@y.z") || strings.Contains(in.Detail, "acme") {
		t.Errorf("PII leaked into detail: %q", in.Detail)
	}

	out := probeAuth(ctx, fakeCLI(map[string]string{
		"claude auth status --json": `{"loggedIn":false}`,
	}), "claude")
	if out.State != "logged-out" {
		t.Errorf("claude logged-out: state = %q, want logged-out", out.State)
	}

	cx := probeAuth(ctx, fakeCLI(map[string]string{
		"codex login status": "Logged in using ChatGPT",
	}), "codex")
	if cx.State != "ok" {
		t.Errorf("codex: state = %q, want ok", cx.State)
	}

	if pi := probeAuth(ctx, fakeCLI(nil), "pi"); pi.State != "unknown" {
		t.Errorf("pi has no auth primitive: state = %q, want unknown", pi.State)
	}
}

func TestProbeModelsPiCountsCatalog(t *testing.T) {
	ctx := context.Background()
	listing := "provider  model  context\nanthropic  claude-opus-4-5  200K\nopenai  gpt-5.6  400K\n"
	got, err := probeModels(ctx, fakeCLI(map[string]string{"pi --list-models": listing}), "pi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "2 models listed") {
		t.Errorf("pi models signal = %q, want 2 counted (header skipped)", got)
	}

	if _, err := probeModels(ctx, fakeCLI(nil), "bogus"); err == nil {
		t.Error("unknown harness: want error")
	}
}
