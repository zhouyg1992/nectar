package antigravity

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

func TestBuildArgs(t *testing.T) {
	adapter := NewAdapter(Config{
		Model:       "gemini-4-carbon-high",
		Effort:      "high",
		Agent:       "reviewer",
		Mode:        "auto",
		Sandbox:     true,
		AutoApprove: true,
	})
	args := adapter.buildArgs("conv-1", "hello", engine.EngineOptions{}, 30*time.Minute)
	want := [][2]string{
		{"-p", "hello"},
		{"--output-format", "stream-json"},
		{"--print-timeout", "1800s"},
		{"--model", "gemini-4-carbon-high"},
		{"--effort", "high"},
		{"--agent", "reviewer"},
		{"--mode", "auto"},
		{"--conversation", "conv-1"},
	}
	for _, pair := range want {
		if !hasArgPair(args, pair[0], pair[1]) {
			t.Fatalf("args %#v missing %q %q", args, pair[0], pair[1])
		}
	}
	if !hasArg(args, "--sandbox") || !hasArg(args, "--dangerously-skip-permissions") {
		t.Fatalf("args = %#v", args)
	}
}

func TestAdapterRunsFakeAGY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "fake-agy.sh")
	body := `#!/bin/sh
if [ "$PWD" != "$EXPECTED_WORKDIR" ]; then
  echo "pwd mismatch: $PWD" >&2
  exit 9
fi
for arg in "$@"; do printf '%s\n' "$arg"; done > "$ARGS_FILE"
printf '%s\n' '{"event":"init","conversation_id":"conv-fake","init":{"cwd":"/tmp"}}'
printf '%s\n' '{"event":"step_update","step_update":{"conversation_id":"conv-fake","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"OK\n"}}'
printf '%s\n' '{"event":"result","result":{"conversation_id":"conv-fake","status":"SUCCESS","response":"OK\n","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARGS_FILE", argsFile)
	t.Setenv("EXPECTED_WORKDIR", dir)

	adapter := NewAdapter(Config{Executable: script, WorkingDir: dir, Timeout: 5 * time.Second})
	events, err := adapter.Start(context.Background(), "say OK", engine.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var (
		session string
		text    strings.Builder
		done    bool
	)
	for event := range events {
		switch event.Type {
		case engine.EventSessionStarted:
			session = event.SessionID
		case engine.EventTextDelta:
			text.WriteString(event.Text)
		case engine.EventDone:
			done = true
		}
	}
	if session != "conv-fake" || text.String() != "OK\n" || !done {
		t.Fatalf("session=%q text=%q done=%v", session, text.String(), done)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if !hasArgPair(args, "-p", "say OK") || !hasArgPair(args, "--output-format", "stream-json") {
		t.Fatalf("args = %#v", args)
	}
}

func TestAuthHint(t *testing.T) {
	hint := authErrorMessage("You are not logged into Antigravity.")
	if !strings.Contains(hint, "Run `agy` interactively") {
		t.Fatalf("hint = %q", hint)
	}
	if got := authErrorMessage("plain failure"); got != "plain failure" {
		t.Fatalf("plain error = %q", got)
	}
}

func TestResumeRequiresConversationID(t *testing.T) {
	adapter := NewAdapter(Config{})
	if _, err := adapter.Resume(context.Background(), " ", "hello", engine.EngineOptions{}); err == nil {
		t.Fatal("expected missing conversation id error")
	}
	if _, err := adapter.Start(context.Background(), " ", engine.EngineOptions{}); err == nil {
		t.Fatal("expected empty prompt error")
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasArgPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}
