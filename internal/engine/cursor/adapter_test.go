package cursor

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

func TestNewAdapterResolvesExecutableAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable fixture is unix-only")
	}
	writeBin := func(t *testing.T, dir, name string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("prefers agent", func(t *testing.T) {
		dir := t.TempDir()
		agent := writeBin(t, dir, "agent")
		writeBin(t, dir, "cursor-agent")
		t.Setenv("PATH", dir)
		adapter := NewAdapter(Config{Executable: "agent"})
		if adapter.cfg.Executable != agent {
			t.Fatalf("executable = %q, want %q", adapter.cfg.Executable, agent)
		}
	})

	t.Run("agent falls back to legacy", func(t *testing.T) {
		dir := t.TempDir()
		legacy := writeBin(t, dir, "cursor-agent")
		t.Setenv("PATH", dir)
		adapter := NewAdapter(Config{Executable: "agent"})
		if adapter.cfg.Executable != legacy {
			t.Fatalf("executable = %q, want %q", adapter.cfg.Executable, legacy)
		}
	})

	t.Run("legacy falls back to agent", func(t *testing.T) {
		dir := t.TempDir()
		agent := writeBin(t, dir, "agent")
		t.Setenv("PATH", dir)
		adapter := NewAdapter(Config{Executable: "cursor-agent"})
		if adapter.cfg.Executable != agent {
			t.Fatalf("executable = %q, want %q", adapter.cfg.Executable, agent)
		}
	})

	t.Run("absolute path unchanged", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "custom-agent")
		adapter := NewAdapter(Config{Executable: path})
		if adapter.cfg.Executable != path {
			t.Fatalf("executable = %q, want %q", adapter.cfg.Executable, path)
		}
	})
}

func TestAdapterRunsFakeCursorAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	t.Setenv("EXPECTED_WORKDIR", dir)
	script := filepath.Join(dir, "fake-cursor-agent.sh")
	body := `#!/bin/sh
if [ "${EXPECTED_WORKDIR:-}" != "" ] && [ "$PWD" != "$EXPECTED_WORKDIR" ]; then
  echo "pwd mismatch: $PWD expected $EXPECTED_WORKDIR" >&2
  exit 9
fi
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess_fake","model":"Auto"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hel"}]},"session_id":"sess_fake","timestamp_ms":1}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"lo"}]},"session_id":"sess_fake","timestamp_ms":2}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]},"session_id":"sess_fake","timestamp_ms":3}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]},"session_id":"sess_fake"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"hello","session_id":"sess_fake","usage":{"inputTokens":10,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":0,"reasoningTokens":1}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := NewAdapter(Config{
		Executable:     script,
		WorkingDir:     dir,
		Timeout:        5 * time.Second,
		AutoApprove:    true,
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
	if got[4].Meta["usage"] != `{"context_tokens":16,"input_tokens":10,"output_tokens":2}` {
		t.Fatalf("usage = %v", got[4].Meta["usage"])
	}
}

func TestAdapterSurfacesProcessError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-cursor-agent.sh")
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
	if !strings.Contains(errText, "agent login") || !strings.Contains(errText, "CURSOR_API_KEY") {
		t.Fatalf("error = %q", errText)
	}
}

func TestAuthErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{name: "authentication required", in: "Error: Authentication required. Please run 'agent login' first.", want: true},
		{name: "not logged in", in: "fatal: not logged in", want: true},
		{name: "api key hint", in: "set CURSOR_API_KEY environment variable", want: true},
		{name: "model error", in: "model unavailable", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := authErrorMessage(tc.in)
			isHint := strings.Contains(got, "agent login")
			if isHint != tc.want {
				t.Fatalf("authErrorMessage(%q) = %q, want hint=%v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuildArgs(t *testing.T) {
	adapter := NewAdapter(Config{
		Executable:     "cursor-agent",
		Model:          "composer-2.5",
		Mode:           "plan",
		Sandbox:        "enabled",
		AutoApprove:    true,
		TrustWorkspace: true,
	})
	start := strings.Join(adapter.buildArgs("", "hello", engine.EngineOptions{}), " ")
	for _, want := range []string{
		"-p",
		"--trust",
		"--output-format stream-json",
		"--stream-partial-output",
		"-f",
		"--sandbox enabled",
		"--mode plan",
		"--model composer-2.5",
		" hello",
	} {
		if !strings.Contains(start, want) {
			t.Fatalf("start args %q missing %q", start, want)
		}
	}

	resume := strings.Join(adapter.buildArgs("sess_123", "again", engine.EngineOptions{}), " ")
	if !strings.Contains(resume, "--resume sess_123") {
		t.Fatalf("resume args = %q", resume)
	}
	if !strings.HasSuffix(resume, " again") {
		t.Fatalf("prompt should remain the final argument: %q", resume)
	}
}

func TestBuildArgsOptionsOverrideConfig(t *testing.T) {
	adapter := NewAdapter(Config{
		Executable: "cursor-agent",
		Model:      "composer-2.5",
		Sandbox:    "enabled",
	})
	args := strings.Join(adapter.buildArgs("", "hello", engine.EngineOptions{
		Model:   "gpt-5.3-codex",
		Sandbox: "disabled",
	}), " ")
	if !strings.Contains(args, "--model gpt-5.3-codex") {
		t.Fatalf("args = %q", args)
	}
	if !strings.Contains(args, "--sandbox disabled") {
		t.Fatalf("args = %q", args)
	}
	if strings.Contains(args, "--trust") || strings.Contains(args, " -f ") {
		t.Fatalf("unexpected approval flags: %q", args)
	}
}
