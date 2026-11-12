package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("ANTHROPIC_MODEL", "")
	if err := os.WriteFile(
		filepath.Join(dir, "settings.json"),
		[]byte(`{"model":"sonnet"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if got := ResolveModel(""); got != "sonnet" {
		t.Fatalf("model = %q", got)
	}
	if got := ResolveModel("claude-opus-4-5"); got != "claude-opus-4-5" {
		t.Fatalf("explicit model = %q", got)
	}
	t.Setenv("ANTHROPIC_MODEL", "opus")
	if got := ResolveModel(""); got != "opus" {
		t.Fatalf("env model = %q", got)
	}
}
