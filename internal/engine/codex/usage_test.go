package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestReadCodexSessionUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "11", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-11-13T14-13-05-thread-1.jsonl")
	content := `{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":17481,"cached_input_tokens":17280,"output_tokens":16,"total_tokens":17497},"model_context_window":996147}}}
{"type":"token_usage_record","payload":{"turn_token_usage":{"input_tokens":18481,"cached_input_tokens":17280,"output_tokens":26,"total_tokens":18507}}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := readCodexSessionUsage("thread-1")
	if got == nil {
		t.Fatal("readCodexSessionUsage() = nil")
	}
	if !got.HasLast || got.Last.InputTokens != 17481 || got.Last.CachedInputTokens != 17280 ||
		got.Last.OutputTokens != 16 || got.Last.TotalTokens != 17497 {
		t.Fatalf("last usage = %#v", got.Last)
	}
	if !got.HasTurn || got.Turn.InputTokens != 18481 || got.Turn.CachedInputTokens != 17280 ||
		got.Turn.OutputTokens != 26 {
		t.Fatalf("turn usage = %#v", got.Turn)
	}
	if got.ContextWindow != 996147 {
		t.Fatalf("context window = %d", got.ContextWindow)
	}
}

func TestApplyCodexSessionUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "11", "13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-11-13T14-13-05-thread-2.jsonl")
	content := `{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":17481,"cached_input_tokens":17280,"output_tokens":16,"total_tokens":17497},"model_context_window":996147}}}
{"type":"token_usage_record","payload":{"turn_token_usage":{"input_tokens":18481,"cached_input_tokens":17280,"output_tokens":26,"total_tokens":18507}}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	ev := engine.Event{Type: engine.EventDone}
	applyCodexSessionUsage(&ev, "thread-2")
	if ev.Meta["usage"] != `{"cached_input_tokens":17280,"context_tokens":17497,"input_tokens":1201,"output_tokens":26}` {
		t.Fatalf("usage = %v", ev.Meta["usage"])
	}
	if ev.Meta["context_window"] != 996147 {
		t.Fatalf("context window = %v", ev.Meta["context_window"])
	}
}
