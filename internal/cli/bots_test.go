package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveScheduleSocketByBot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bots.one]
platform = "telegram"
engine = "codex"
[bots.one.telegram]
bot_token = "token-one"

[bots.two]
platform = "telegram"
engine = "claude"
[bots.two.telegram]
bot_token = "token-two"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	socket, bot, hasBot, err := resolveScheduleSocket("", path, "two")
	if err != nil {
		t.Fatal(err)
	}
	if !hasBot || bot.ID != "two" {
		t.Fatalf("bot = %#v", bot)
	}
	if !strings.HasSuffix(socket, filepath.Join("bots", "two", "run", "api.sock")) {
		t.Fatalf("socket = %q", socket)
	}
	if _, _, _, err := resolveScheduleSocket("", path, ""); err == nil {
		t.Fatal("expected ambiguous bot error")
	}
}

func TestLoadConfigForBot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bots.reviewer]
name = "Reviewer"
platform = "telegram"
engine = "claude"
[bots.reviewer.telegram]
bot_token = "token-reviewer"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfigForBot(path, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Name != "Reviewer" || cfg.Bot.Platform != "telegram" {
		t.Fatalf("bot config = %#v", cfg.Bot)
	}
}
