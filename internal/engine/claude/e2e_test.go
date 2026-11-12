package claude

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
)

// TestClaudeEndToEnd exercises the installed CLI. It is skipped unless the
// developer opts in, because it needs a logged-in Claude Code account.
func TestClaudeEndToEnd(t *testing.T) {
	if os.Getenv("NECTAR_CLAUDE_E2E") == "" {
		t.Skip("set NECTAR_CLAUDE_E2E=1 to run against the real claude CLI")
	}
	executable := os.Getenv("CLAUDE_PATH")
	if executable == "" {
		executable = "claude"
	}

	adapter := NewAdapter(Config{
		Executable:     executable,
		WorkingDir:     t.TempDir(),
		Timeout:        2 * time.Minute,
		PermissionMode: "plan",
	})
	opts := engine.EngineOptions{Timeout: 2 * time.Minute}

	events, err := adapter.Start(context.Background(), "Reply with exactly: ok", opts)
	text, sessionID, done, errText := collect(t, events, err)
	if errText != "" {
		t.Fatalf("start error: %s", errText)
	}
	if !done {
		t.Fatal("start did not emit done")
	}
	if sessionID == "" {
		t.Fatal("start did not emit a session id")
	}
	if !strings.Contains(strings.ToLower(text), "ok") {
		t.Fatalf("start text = %q", text)
	}

	events, err = adapter.Resume(
		context.Background(),
		sessionID,
		"What word did I ask you to reply? Reply with just that word.",
		opts,
	)
	resumeText, resumedID, resumeDone, resumeErr := collect(t, events, err)
	if resumeErr != "" {
		t.Fatalf("resume error: %s", resumeErr)
	}
	if !resumeDone {
		t.Fatal("resume did not emit done")
	}
	if resumedID != sessionID {
		t.Fatalf("resumed session = %q, want %q", resumedID, sessionID)
	}
	if !strings.Contains(strings.ToLower(resumeText), "ok") {
		t.Fatalf("resume text = %q", resumeText)
	}
}

func collect(t *testing.T, events <-chan engine.Event, err error) (string, string, bool, string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var sessionID string
	var done bool
	var errTexts []string
	for event := range events {
		switch event.Type {
		case engine.EventSessionStarted:
			sessionID = event.SessionID
		case engine.EventTextDelta:
			text.WriteString(event.Text)
		case engine.EventMessage:
			text.Reset()
			text.WriteString(event.Text)
		case engine.EventDone:
			done = true
		case engine.EventError:
			errTexts = append(errTexts, event.Text)
		}
	}
	return text.String(), sessionID, done, strings.Join(errTexts, "; ")
}
