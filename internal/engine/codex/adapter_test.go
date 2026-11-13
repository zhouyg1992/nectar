package codex

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

func TestAdapterRunsFakeCodex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-codex.sh")
	body := `#!/bin/sh
read line
printf '{"type":"thread.started","thread_id":"t-fake"}\n'
printf '{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"%s"}}\n' "$line"
printf '{"type":"turn.completed","usage":{"input_tokens":1}}\n'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := NewAdapter(Config{
		Executable: script,
		WorkingDir: dir,
		Timeout:    5 * time.Second,
	})
	events, err := adapter.Start(context.Background(), "hello prompt", engine.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var got []engine.Event
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) != 3 {
		t.Fatalf("events = %#v", got)
	}
	if got[0].Type != engine.EventSessionStarted || got[0].SessionID != "t-fake" {
		t.Fatalf("session event = %#v", got[0])
	}
	if got[1].Type != engine.EventMessage || got[1].Text != "hello prompt" {
		t.Fatalf("message event = %#v", got[1])
	}
	if got[2].Type != engine.EventDone {
		t.Fatalf("done event = %#v", got[2])
	}
}

func TestAdapterSurfacesProcessError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-codex.sh")
	body := `#!/bin/sh
cat >/dev/null
printf '{"type":"thread.started","thread_id":"t-bad"}\n'
echo 'fatal: no auth' >&2
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
	for ev := range events {
		if ev.Type == engine.EventError {
			errText = ev.Text
		}
	}
	if !strings.Contains(errText, "no auth") {
		t.Fatalf("error event = %q", errText)
	}
}

func TestAdapterAppliesSessionTokenUsage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-codex.sh")
	body := `#!/bin/sh
cat >/dev/null
printf '{"type":"thread.started","thread_id":"thread-1"}\n'
printf '{"type":"turn.completed","usage":{"input_tokens":10016673,"output_tokens":75311}}\n'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(home, "sessions", "2026", "11", "13")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	session := `{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":17481,"cached_input_tokens":17280,"output_tokens":16,"total_tokens":17497},"model_context_window":996147}}}
{"type":"token_usage_record","payload":{"turn_token_usage":{"input_tokens":17481,"cached_input_tokens":17280,"output_tokens":16,"total_tokens":17497}}}
`
	if err := os.WriteFile(filepath.Join(sessionDir, "rollout-2026-11-13T14-13-05-thread-1.jsonl"), []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := NewAdapter(Config{Executable: script, WorkingDir: dir, Timeout: 5 * time.Second})
	events, err := adapter.Start(context.Background(), "ping", engine.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var got []engine.Event
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) != 2 || got[1].Type != engine.EventDone {
		t.Fatalf("events = %#v", got)
	}
	if got[1].Meta["usage"] != `{"cached_input_tokens":17280,"context_tokens":17497,"input_tokens":201,"output_tokens":16}` {
		t.Fatalf("usage = %v", got[1].Meta["usage"])
	}
	if got[1].Meta["context_window"] != 996147 {
		t.Fatalf("context window = %v", got[1].Meta["context_window"])
	}
}
