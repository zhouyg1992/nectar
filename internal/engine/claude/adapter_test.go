package claude

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestAdapterRunsFakeClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	t.Setenv("EXPECTED_WORKDIR", dir)
	t.Setenv("EXPECTED_PROMPT", "hello")
	script := filepath.Join(dir, "fake-claude.sh")
	body := `#!/bin/sh
if [ "${EXPECTED_WORKDIR:-}" != "" ] && [ "$PWD" != "$EXPECTED_WORKDIR" ]; then
  echo "pwd mismatch: $PWD expected $EXPECTED_WORKDIR" >&2
  exit 9
fi
prompt=$(cat)
if [ "$prompt" != "${EXPECTED_PROMPT:-}" ]; then
  echo "prompt mismatch: $prompt" >&2
  exit 8
fi
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess_fake","model":"claude-sonnet-4-5"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}},"session_id":"sess_fake"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}},"session_id":"sess_fake"}'
printf '%s\n' '{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":4,"output_tokens":1}},"session_id":"sess_fake"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"message_delta","usage":{"input_tokens":4,"output_tokens":1,"cache_read_input_tokens":6}},"session_id":"sess_fake"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"hello","session_id":"sess_fake","usage":{"input_tokens":4,"output_tokens":1,"cache_read_input_tokens":6},"modelUsage":{"claude-sonnet-4-5":{"inputTokens":4,"outputTokens":1,"cacheReadInputTokens":6,"contextWindow":200000}}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := NewAdapter(Config{
		Executable:  script,
		WorkingDir:  dir,
		Timeout:     5 * time.Second,
		AutoApprove: true,
	})
	events, err := adapter.Start(context.Background(), "hello", engine.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var got []engine.Event
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 5 {
		t.Fatalf("events = %#v", got)
	}
	if got[0].Type != engine.EventSessionStarted || got[0].SessionID != "sess_fake" {
		t.Fatalf("session = %#v", got[0])
	}
	if got[1].Type != engine.EventTextDelta || got[1].Text != "hel" {
		t.Fatalf("first delta = %#v", got[1])
	}
	if got[2].Type != engine.EventTextDelta || got[2].Text != "lo" {
		t.Fatalf("second delta = %#v", got[2])
	}
	if got[3].Type != engine.EventMessage || got[3].Text != "hello" {
		t.Fatalf("message = %#v", got[3])
	}
	if got[4].Type != engine.EventDone {
		t.Fatalf("done = %#v", got[4])
	}
	if got[4].Meta["usage"] != `{"cached_input_tokens":6,"context_tokens":11,"input_tokens":4,"output_tokens":1}` {
		t.Fatalf("usage = %v", got[4].Meta["usage"])
	}
	if got[4].Meta["context_window"] != 200000 {
		t.Fatalf("context window = %v", got[4].Meta["context_window"])
	}
}

func TestAdapterSurfacesProcessError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-claude.sh")
	body := `#!/bin/sh
cat >/dev/null
echo 'fatal: not logged in' >&2
exit 7
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := NewAdapter(Config{Executable: script, WorkingDir: dir, Timeout: 5 * time.Second})
	events, err := adapter.Start(context.Background(), "hello", engine.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var errText string
	for event := range events {
		if event.Type == engine.EventError {
			errText = event.Text
		}
	}
	if !strings.Contains(errText, "not logged in") {
		t.Fatalf("error = %q", errText)
	}
}

func TestBuildArgs(t *testing.T) {
	adapter := NewAdapter(Config{
		Executable:     "claude",
		Model:          "claude-sonnet-4-5",
		FallbackModel:  "claude-haiku-4-5",
		PermissionMode: "plan",
		AutoApprove:    true,
		MaxTurns:       12,
	})
	start := strings.Join(adapter.buildArgs("", engine.EngineOptions{}), " ")
	for _, want := range []string{
		"-p",
		"--output-format stream-json",
		"--verbose",
		"--include-partial-messages",
		"--dangerously-skip-permissions",
		"--model claude-sonnet-4-5",
		"--fallback-model claude-haiku-4-5",
		"--max-turns 12",
	} {
		if !strings.Contains(start, want) {
			t.Fatalf("start args %q missing %q", start, want)
		}
	}
	if strings.Contains(start, "--permission-mode") {
		t.Fatalf("auto approve should replace permission mode: %q", start)
	}

	resume := strings.Join(adapter.buildArgs("sess_123", engine.EngineOptions{}), " ")
	if !strings.Contains(resume, "--resume sess_123") {
		t.Fatalf("resume args = %q", resume)
	}
}

func TestBuildArgsPermissionModeAndOptionsOverride(t *testing.T) {
	adapter := NewAdapter(Config{
		Executable:     "claude",
		Model:          "claude-sonnet-4-5",
		PermissionMode: "acceptEdits",
	})
	args := strings.Join(adapter.buildArgs("", engine.EngineOptions{
		Model: "claude-opus-4-5",
	}), " ")
	if !strings.Contains(args, "--permission-mode acceptEdits") {
		t.Fatalf("args = %q", args)
	}
	if !strings.Contains(args, "--model claude-opus-4-5") {
		t.Fatalf("args = %q", args)
	}
	if strings.Contains(args, "--dangerously-skip-permissions") {
		t.Fatalf("unexpected bypass flag: %q", args)
	}
}
