package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveContextWindowFromCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	config := `model = "deepseek/deepseek-flash"
model_catalog_json = "models.json"
`
	catalog := `{"models":[{"slug":"deepseek/deepseek-flash","context_window":258000}]}`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "models.json"), []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := ResolveContextWindow("", 0); got != 258000 {
		t.Fatalf("ResolveContextWindow() = %d, want 258000", got)
	}
	if got := ResolveContextWindow("", 200000); got != 200000 {
		t.Fatalf("override = %d, want 200000", got)
	}
	if got := ResolveContextWindow("missing/model", 0); got != 0 {
		t.Fatalf("unknown model = %d, want 0", got)
	}
}

func TestResolveContextWindowWithoutConfig(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	if got := ResolveContextWindow("", 0); got != 0 {
		t.Fatalf("ResolveContextWindow() = %d, want 0", got)
	}
}
