package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(
		filepath.Join(home, "config.toml"),
		[]byte("model = \"deepseek/deepseek-flash\"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if got := ResolveModel(""); got != "deepseek/deepseek-flash" {
		t.Fatalf("ResolveModel() = %q", got)
	}
	if got := ResolveModel("gpt-explicit"); got != "gpt-explicit" {
		t.Fatalf("explicit model = %q", got)
	}
}
