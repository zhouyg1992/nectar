package pi

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

func TestAdapterRunsFakePi(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	t.Setenv("EXPECTED_WORKDIR", dir)
	t.Setenv("EXPECTED_PROMPT", "hello")
	script := filepath.Join(dir, "fake-pi.sh")
	body := `#!/bin/sh
if [ "${EXPECTED_WORKDIR:-}" != "" ] && [ "$PWD" != "$EXPECTED_WORKDIR" ]; then
  echo "pwd mismatch: $PWD expected $EXPECTED_WORKDIR" >&2
  exit 9
fi
last=""
last=$(cat)
if [ "$last" != "${EXPECTED_PROMPT:-}" ]; then
  echo "prompt mismatch: $last" >&2
  exit 8
fi
printf '%s\n' '{"type":"session","version":3,"id":"sess_fake","cwd":"/tmp"}'
printf '%s\n' '{"type":"message_update","usage":{"input":4,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":5},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hel"}}'
printf '%s\n' '{"type":"message_update","usage":{"input":4,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":6},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"lo"}}'
printf '%s\n' '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"usage":{"input":4,"output":2,"cacheRead":6,"cacheWrite":0,"totalTokens":12},"stopReason":"stop"}}'
printf '%s\n' '{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"hello"}],"usage":{"input":4,"output":2,"cacheRead":6,"cacheWrite":0,"totalTokens":12},"stopReason":"stop"}]}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := NewAdapter(Config{
		Executable:     script,
		WorkingDir:     dir,
		Timeout:        5 * time.Second,
		TrustWorkspace: true,
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
	if got[4].Meta["usage"] != `{"cached_input_tokens":6,"context_tokens":12,"input_tokens":4,"output_tokens":2}` {
		t.Fatalf("usage = %v", got[4].Meta["usage"])
	}
}

func TestAdapterSurfacesProcessError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-pi.sh")
	body := `#!/bin/sh
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
		Executable:     "pi",
		Provider:       "anthropic",
		Model:          "claude-sonnet-4-5",
		Thinking:       "high",
		TrustWorkspace: true,
	})
	start := strings.Join(adapter.buildArgs("", engine.EngineOptions{}), " ")
	for _, want := range []string{
		"--mode json",
		"--approve",
		"--provider anthropic",
		"--model claude-sonnet-4-5",
		"--thinking high",
	} {
		if !strings.Contains(start, want) {
			t.Fatalf("start args %q missing %q", start, want)
		}
	}

	resume := strings.Join(adapter.buildArgs("sess_123", engine.EngineOptions{}), " ")
	if !strings.Contains(resume, "--session sess_123") {
		t.Fatalf("resume args = %q", resume)
	}
}

func TestBuildArgsOptionsOverrideModel(t *testing.T) {
	adapter := NewAdapter(Config{Executable: "pi", Model: "default-model"})
	args := strings.Join(adapter.buildArgs("", engine.EngineOptions{Model: "override-model"}), " ")
	if !strings.Contains(args, "--model override-model") {
		t.Fatalf("args = %q", args)
	}
	if strings.Contains(args, "--approve") {
		t.Fatalf("unexpected approve flag: %q", args)
	}
}

func TestResolveModelFromSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultProvider":"anthropic","defaultModel":"claude-sonnet-4-5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveModel(""); got != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("ResolveModel() = %q", got)
	}
	if got := ResolveModel("openai/gpt-5"); got != "openai/gpt-5" {
		t.Fatalf("explicit ResolveModel() = %q", got)
	}
}
