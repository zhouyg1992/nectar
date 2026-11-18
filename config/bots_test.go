package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveBotsKeepsLegacyDefault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bot]
name = "Legacy"
engine = "codex"
platform = "feishu"

[feishu]
app_id = "cli_legacy"
app_secret = "secret_legacy"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 {
		t.Fatalf("bots = %#v", bots)
	}
	bot := bots[0]
	if bot.ID != "default" || bot.Name != "Legacy" {
		t.Fatalf("legacy bot = %#v", bot)
	}
	if bot.Config.Storage.DataDir != root {
		t.Fatalf("legacy data dir = %q", bot.Config.Storage.DataDir)
	}
	if bot.Config.Bot.WorkspaceMode != "shared" {
		t.Fatalf("legacy workspace mode = %q", bot.Config.Bot.WorkspaceMode)
	}
}

func TestResolveBotsEntriesAreIsolated(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bots.codex_dev]
platform = "feishu"
engine = "codex"
workspace = "` + filepath.Join(root, "project-a") + `"
workspace_mode = "worktree"

[bots.codex_dev.feishu]
app_id = "cli_a"
app_secret = "secret_a"

[bots.claude_review]
platform = "telegram"
engine = "claude"
workspace = "` + filepath.Join(root, "project-b") + `"
model = "claude-sonnet"

[bots.claude_review.telegram]
bot_token = "token_b"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("bots = %#v", bots)
	}
	byID := map[string]ResolvedBot{}
	for _, bot := range bots {
		byID[bot.ID] = bot
	}
	codex := byID["codex_dev"]
	if codex.Config.Bot.Platform != "feishu" || codex.Config.Bot.Engine != "codex" {
		t.Fatalf("codex bot = %#v", codex.Config.Bot)
	}
	if codex.Config.Storage.DataDir != filepath.Join(root, "bots", "codex_dev") {
		t.Fatalf("codex data dir = %q", codex.Config.Storage.DataDir)
	}
	if codex.Config.Bot.WorkspaceMode != "worktree" {
		t.Fatalf("codex workspace mode = %q", codex.Config.Bot.WorkspaceMode)
	}
	if codex.Config.Codex.WorkingDir != filepath.Join(root, "project-a") {
		t.Fatalf("codex workspace = %q", codex.Config.Codex.WorkingDir)
	}
	claude := byID["claude_review"]
	if claude.Config.Bot.Platform != "telegram" || claude.Config.Bot.Engine != "claude" {
		t.Fatalf("claude bot = %#v", claude.Config.Bot)
	}
	if claude.Config.Claude.Model != "claude-sonnet" {
		t.Fatalf("claude model = %q", claude.Config.Claude.Model)
	}
	if claude.Config.Claude.WorkingDir != filepath.Join(root, "project-b") {
		t.Fatalf("claude workspace = %q", claude.Config.Claude.WorkingDir)
	}
}

func TestResolveBotsSingleEntryUsesStableBotDataDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bots.solo]
platform = "telegram"
engine = "codex"

[bots.solo.telegram]
bot_token = "solo-token"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].ID != "solo" {
		t.Fatalf("bots = %#v", bots)
	}
	want := filepath.Join(root, "bots", "solo")
	if bots[0].Config.Storage.DataDir != want {
		t.Fatalf("solo data dir = %q, want %q", bots[0].Config.Storage.DataDir, want)
	}
}

func TestResolvedBotDataDirDoesNotChangeWhenAddingSecondBot(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Storage: StorageConfig{DataDir: root}, Bots: map[string]BotEntry{
		"first": {Platform: "telegram", Engine: "codex", Telegram: &TelegramConfig{BotToken: "token-first"}},
	}}
	before, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Bots["second"] = BotEntry{Platform: "telegram", Engine: "claude", Telegram: &TelegramConfig{BotToken: "token-second"}}
	after, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || len(after) != 2 {
		t.Fatalf("bots before=%d after=%d", len(before), len(after))
	}
	if before[0].Config.Storage.DataDir != after[0].Config.Storage.DataDir {
		t.Fatalf("data dir changed: %q -> %q", before[0].Config.Storage.DataDir, after[0].Config.Storage.DataDir)
	}
}

func TestResolveBotsIncludesLegacyAndEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bot]
name = "Legacy"
engine = "codex"
platform = "feishu"

[feishu]
app_id = "cli_legacy"
app_secret = "secret_legacy"

[bots.reviewer]
platform = "feishu"
engine = "claude"

[bots.reviewer.feishu]
app_id = "cli_reviewer"
app_secret = "secret_reviewer"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("bots = %#v", bots)
	}
	if bots[0].ID != "default" || bots[1].ID != "reviewer" {
		t.Fatalf("bot order = %#v", bots)
	}
}

func TestResolveBotsRejectsDuplicateCredentials(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bots.one]
platform = "feishu"
engine = "codex"
[bots.one.feishu]
app_id = "cli_same"
app_secret = "secret_one"

[bots.two]
platform = "feishu"
engine = "claude"
[bots.two.feishu]
app_id = "cli_same"
app_secret = "secret_two"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.ResolveBots()
	if err == nil || !strings.Contains(err.Error(), "same feishu credentials") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveBotsRequiresPlatformCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[bots.empty]
platform = "feishu"
engine = "codex"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ResolveBots(); err == nil {
		t.Fatal("expected missing credentials error")
	}
}

func TestResolveBotsSupportsDingTalkAndTelegram(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bots.ding]
platform = "dingtalk"
engine = "codex"

[bots.ding.dingtalk]
client_id = "ding_client"
client_secret = "ding_secret"

[bots.tg]
platform = "telegram"
engine = "claude"

[bots.tg.telegram]
bot_token = "telegram_token"
proxy = "http://127.0.0.1:7897"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 {
		t.Fatalf("bots = %#v", bots)
	}
	byID := map[string]ResolvedBot{}
	for _, bot := range bots {
		byID[bot.ID] = bot
	}
	if got := byID["ding"]; got.Config.Bot.Platform != "dingtalk" || got.Config.DingTalk.RobotCode != "ding_client" {
		t.Fatalf("dingtalk bot = %#v", got)
	}
	if got := byID["tg"]; got.Config.Bot.Platform != "telegram" || got.Config.Telegram.Proxy != "http://127.0.0.1:7897" {
		t.Fatalf("telegram bot = %#v", got)
	}
}

func TestBotEntryInheritsLegacyEngine(t *testing.T) {
	cfg := Config{
		Bot:     BotConfig{Engine: "claude"},
		Storage: StorageConfig{DataDir: t.TempDir()},
		Bots: map[string]BotEntry{
			"reviewer": {
				Platform: "telegram",
				Telegram: &TelegramConfig{BotToken: "token-reviewer"},
			},
		},
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].Config.Bot.Engine != "claude" {
		t.Fatalf("bots = %#v", bots)
	}
}

func TestResolveBotsCarriesRelayRoles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bots.coder]
name = "Coder"
platform = "feishu"
engine = "codex"
role = "coder"
description = "writes code"
capabilities = ["code", " test "]
relay_accept_from = [" reviewer "]

[bots.coder.feishu]
app_id = "cli_coder"
app_secret = "{{SECRET_coder}}"

[bots.reviewer]
name = "Reviewer"
platform = "telegram"
engine = "claude"
role = "reviewer"
relay_accept_from = ["coder"]

[bots.reviewer.telegram]
bot_token = "{{SECRET_reviewer}}"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ResolvedBot{}
	for _, bot := range bots {
		byID[bot.ID] = bot
	}
	coder := byID["coder"].Config.Bot
	if coder.Role != "coder" || coder.Description != "writes code" {
		t.Fatalf("coder role = %#v", coder)
	}
	if len(coder.Capabilities) != 2 || coder.Capabilities[1] != "test" {
		t.Fatalf("coder capabilities = %#v", coder.Capabilities)
	}
	if len(coder.RelayAcceptFrom) != 1 || coder.RelayAcceptFrom[0] != "reviewer" {
		t.Fatalf("coder relay allowlist = %#v", coder.RelayAcceptFrom)
	}
}

func TestResolveBotsRejectsUnknownRelayAcceptFrom(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	body := `
[bots.coder]
platform = "feishu"
engine = "codex"
relay_accept_from = ["ghost"]

[bots.coder.feishu]
app_id = "cli_coder"
app_secret = "{{SECRET_coder2}}"

[storage]
data_dir = "` + root + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ResolveBots(); err == nil || !strings.Contains(err.Error(), "unknown bot") {
		t.Fatalf("expected unknown relay_accept_from error, got %v", err)
	}
}
