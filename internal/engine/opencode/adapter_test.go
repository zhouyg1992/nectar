package opencode

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

func TestAdapterRunsFakeOpenCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	t.Setenv("EXPECTED_WORKDIR", dir)
	script := filepath.Join(dir, "fake-opencode.sh")
	body := `#!/bin/sh
if [ "${EXPECTED_WORKDIR:-}" != "" ] && [ "$PWD" != "$EXPECTED_WORKDIR" ]; then
  echo "pwd mismatch: $PWD expected $EXPECTED_WORKDIR" >&2
  exit 9
fi
case "$*" in
  *"session export"*)
    printf '{"info":{"tokens":{"input":300,"output":50}},"messages":[{"type":"assistant","tokens":{"input":120,"output":30,"reasoning":5,"cache":{"read":25,"write":0}}},{"type":"idle"}]}\n'
    ;;
  *)
    printf '{"type":"text","sessionID":"ses_fake","part":{"type":"text","text":"hello from opencode"}}\n'
    ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := NewAdapter(Config{
		Executable:  script,
		WorkingDir:  dir,
		Timeout:     5 * time.Second,
		Standalone:  true,
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
	if len(got) != 3 {
		t.Fatalf("events = %#v", got)
	}
	if got[0].Type != engine.EventSessionStarted || got[0].SessionID != "ses_fake" {
		t.Fatalf("session = %#v", got[0])
	}
	if got[1].Type != engine.EventMessage || got[1].Text != "hello from opencode" {
		t.Fatalf("message = %#v", got[1])
	}
	if got[2].Type != engine.EventDone {
		t.Fatalf("done = %#v", got[2])
	}
	if got[2].Meta["usage"] != `{"context_tokens":180,"input_tokens":120,"output_tokens":30}` {
		t.Fatalf("usage = %v", got[2].Meta["usage"])
	}
}

func TestAdapterSurfacesProcessError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-opencode.sh")
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
		Executable:  "opencode",
		Model:       "provider/model",
		Agent:       "build",
		Standalone:  true,
		AutoApprove: true,
	})
	start := strings.Join(adapter.buildArgs("", "hello", engine.EngineOptions{}), " ")
	for _, want := range []string{"run --format json", "--standalone", "--auto", "--model provider/model", "--agent build", " hello"} {
		if !strings.Contains(start, want) {
			t.Fatalf("start args %q missing %q", start, want)
		}
	}
	resume := strings.Join(adapter.buildArgs("ses_123", "again", engine.EngineOptions{}), " ")
	if !strings.Contains(resume, "--session ses_123") {
		t.Fatalf("resume args = %q", resume)
	}
}

func TestResolveModel(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("OPENCODE_CONFIG", "")
	dir := filepath.Join(configHome, "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "opencode.json"),
		[]byte(`{"model":"magpie/deepseek/deepseek-flash"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if got := ResolveModel(""); got != "magpie/deepseek/deepseek-flash" {
		t.Fatalf("ResolveModel() = %q", got)
	}
	if got := ResolveModel("explicit/model"); got != "explicit/model" {
		t.Fatalf("explicit model = %q", got)
	}
}
