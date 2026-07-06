package cockpit

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// TestSendChatHistoryExcludesSystemAndCurrentTurn is the fix's proof for the
// critical finding: sendChat used to copy every m.turns entry verbatim,
// including "system" turns (slash-command output, chat errors) and the
// just-appended current user turn. The backend forwards roles verbatim to
// the Anthropic API, which 400s on any role other than user/assistant, so a
// leaked "system" turn broke chat permanently after the first /help; and
// the daemon appends the current message itself, so including it here
// duplicated the question.
//
// This drives the model through /help (which records a "system" turn) and
// then a free-text message, capturing the history passed to a fake chatFn.
func TestSendChatHistoryExcludesSystemAndCurrentTurn(t *testing.T) {
	m := testModel()

	var gotHistory []apiclient.ChatTurn
	m.chatFn = func(ctx context.Context, userMsg string, history []apiclient.ChatTurn) (string, error) {
		gotHistory = history
		return "reply to " + userMsg, nil
	}

	// /help records a "user" turn (the command text) and a "system" turn
	// (the command's help text) — neither should reach the LLM.
	if cmd := m.submit("/help"); cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}

	const question = "what's my exposure?"
	cmd := m.submit(question)
	if cmd == nil {
		t.Fatal("submit of free text returned nil cmd")
	}
	// submit batches sendChat with the spinner tick; unwrap the batch and
	// run sendChat's cmd directly so this test doesn't wait on the spinner.
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("expected a non-empty tea.BatchMsg, got %T: %+v", msg, msg)
	}
	batch[0]() // runs sendChat's cmd, populating gotHistory via chatFn

	if gotHistory == nil {
		t.Fatal("chatFn was never called")
	}
	for _, turn := range gotHistory {
		if turn.Role == "system" {
			t.Errorf("history leaked a system turn: %+v", turn)
		}
		if turn.Role == "user" && turn.Text == question {
			t.Errorf("history contains the current message being sent: %+v", turn)
		}
	}
}

// TestSubmitBusyGuardIgnoresFreeText covers the busy-guard fix: while a chat
// call is in flight, a second free-text submit must be ignored entirely —
// no duplicate user turn, no cmd, matching "ignore whole input" semantics
// (slash commands are unaffected and may still run while busy).
func TestSubmitBusyGuardIgnoresFreeText(t *testing.T) {
	m := testModel()
	m.chatFn = func(ctx context.Context, userMsg string, history []apiclient.ChatTurn) (string, error) {
		return "reply", nil
	}

	cmd := m.submit("first message")
	if cmd == nil {
		t.Fatal("first submit returned nil cmd")
	}
	if !m.busy {
		t.Fatal("busy flag not set after first submit")
	}
	turnsAfterFirst := len(m.turns)

	cmd2 := m.submit("second message while busy")
	if cmd2 != nil {
		t.Error("second submit while busy should return nil cmd")
	}
	if len(m.turns) != turnsAfterFirst {
		t.Errorf("busy submit added turns: got %d, want %d", len(m.turns), turnsAfterFirst)
	}
}

// TestChatHistoryFiltersSystemRole unit-tests the filtering helper in
// isolation from submit/sendChat wiring.
func TestChatHistoryFiltersSystemRole(t *testing.T) {
	turns := []apiclient.ChatTurn{
		{Role: "user", Text: "/help"},
		{Role: "system", Text: "slash commands: ..."},
		{Role: "user", Text: "hello"},
		{Role: "assistant", Text: "hi there"},
	}
	got := chatHistory(turns)
	want := []apiclient.ChatTurn{
		{Role: "user", Text: "/help"},
		{Role: "user", Text: "hello"},
		{Role: "assistant", Text: "hi there"},
	}
	if len(got) != len(want) {
		t.Fatalf("chatHistory() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chatHistory()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
