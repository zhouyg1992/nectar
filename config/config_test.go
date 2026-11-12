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

func TestEngineSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\nengine = \"opencode\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Engine != "opencode" || cfg.Bot.AgentID != "opencode" {
		t.Fatalf("bot = %#v", cfg.Bot)
	}
	if cfg.OpenCode.Path != "opencode" || cfg.OpenCode.MaxOutputBytes <= 0 {
		t.Fatalf("opencode defaults = %#v", cfg.OpenCode)
	}
}

func TestLegacyAgentIDSelectsEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\nagent_id = \"opencode\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Engine != "opencode" {
		t.Fatalf("engine = %q", cfg.Bot.Engine)
	}
}

func TestOpenCodeContextWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[opencode]\ncontext_window = 200000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenCode.ContextWindow != 200000 {
		t.Fatalf("opencode context window = %d", cfg.OpenCode.ContextWindow)
	}
}

func TestInvalidEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\nengine = \"unknown\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unsupported engine error")
	}
}

func TestCursorEngineSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(
		"[bot]\nengine = \"cursor\"\n\n[cursor]\ncontext_window = 200000\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Engine != "cursor" || cfg.Bot.AgentID != "cursor" {
		t.Fatalf("bot = %#v", cfg.Bot)
	}
	if cfg.Cursor.Path != "cursor-agent" || cfg.Cursor.MaxOutputBytes <= 0 {
		t.Fatalf("cursor defaults = %#v", cfg.Cursor)
	}
	if !cfg.Cursor.AutoApprove || !cfg.Cursor.TrustWorkspace {
		t.Fatalf("cursor non-interactive defaults = %#v", cfg.Cursor)
	}
	if cfg.Cursor.ContextWindow != 200000 {
		t.Fatalf("cursor context window = %d", cfg.Cursor.ContextWindow)
	}
	timeout, err := cfg.CursorTimeout()
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 30*time.Minute {
		t.Fatalf("timeout = %v", timeout)
	}
}

func TestCursorRejectsInvalidModeAndSandbox(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"mode":    "[cursor]\nmode = \"yolo\"\n",
		"sandbox": "[cursor]\nsandbox = \"maybe\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".toml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
