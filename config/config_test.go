package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsAndEnv(t *testing.T) {
	t.Setenv("FEISHU_APP_ID", "cli_env")
	t.Setenv("FEISHU_APP_SECRET", "secret_env")
	t.Setenv("NECTAR_DATA_DIR", t.TempDir())

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Feishu.AppID != "cli_env" || cfg.Feishu.AppSecret != "secret_env" {
		t.Fatalf("env override failed: %#v", cfg.Feishu)
	}
	if cfg.Bot.AgentID != "codex" || cfg.Bot.Name != "Nectar" {
		t.Fatalf("bot defaults = %#v", cfg.Bot)
	}
	timeout, err := cfg.Timeout()
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 30*time.Minute {
		t.Fatalf("timeout = %v", timeout)
	}
}

func TestLoadRejectsInvalidTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[codex]\ntimeout = \"soon\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}
