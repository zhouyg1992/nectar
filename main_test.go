package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/attachment"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestSetupOpenCodeEngine(t *testing.T) {
	cfg := config.Config{
		Bot: config.BotConfig{Engine: "opencode"},
		OpenCode: config.OpenCodeConfig{
			Path:           "opencode",
			Model:          "provider/model",
			Agent:          "build",
			WorkingDir:     "/tmp/nectar-opencode",
			Timeout:        "10m",
			MaxOutputBytes: 4096,
			ContextWindow:  200000,
			Standalone:     true,
			AutoApprove:    true,
		},
	}
	setup, err := setupEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if setup.adapter == nil {
		t.Fatal("missing engine adapter")
	}
	if setup.name != "opencode" || setup.model != "provider/model" {
		t.Fatalf("setup = %#v", setup)
	}
	if setup.contextWindow != 200000 {
		t.Fatalf("context window = %d", setup.contextWindow)
	}
	if setup.options.WorkingDir != "/tmp/nectar-opencode" || setup.options.Timeout != 10*time.Minute {
		t.Fatalf("options = %#v", setup.options)
	}
}

func TestSetupPiEngine(t *testing.T) {
	cfg := config.Config{
		Bot: config.BotConfig{Engine: "pi"},
		Pi: config.PiConfig{
			Path:           "pi",
			Provider:       "anthropic",
			Model:          "claude-sonnet-4-5",
			Thinking:       "high",
			WorkingDir:     "/tmp/nectar-pi",
			Timeout:        "10m",
			MaxOutputBytes: 4096,
			ContextWindow:  200000,
			TrustWorkspace: true,
		},
	}
	setup, err := setupEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if setup.adapter == nil {
		t.Fatal("missing engine adapter")
	}
	if setup.name != "pi" || setup.model != "claude-sonnet-4-5" {
		t.Fatalf("setup = %#v", setup)
	}
	if setup.contextWindow != 200000 {
		t.Fatalf("context window = %d", setup.contextWindow)
	}
	if setup.options.WorkingDir != "/tmp/nectar-pi" || setup.options.Timeout != 10*time.Minute {
		t.Fatalf("options = %#v", setup.options)
	}
	if setup.adapter.Name() != "pi" {
		t.Fatalf("adapter name = %q", setup.adapter.Name())
	}
}

func TestNewTelegramPlatformAdapter(t *testing.T) {
	cfg := config.Config{
		Bot: config.BotConfig{
			Platform:   "telegram",
			AdminUsers: []string{"42"},
		},
		Telegram: config.TelegramConfig{
			BotToken: "placeholder",
			Proxy:    "http://127.0.0.1:7890",
		},
	}
	adapter, name, err := newPlatformAdapter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if name != "telegram" || adapter.Name() != "telegram" {
		t.Fatalf("adapter = %q name = %q", adapter.Name(), name)
	}
	caps := adapter.Capabilities()
	if !caps.Card || !caps.Streaming || !caps.Media || !caps.Group {
		t.Fatalf("capabilities = %#v", caps)
	}
}

func TestMultiBotPlatformAdapters(t *testing.T) {
	cfg := config.Config{
		Storage: config.StorageConfig{DataDir: t.TempDir()},
		Bots: map[string]config.BotEntry{
			"fs": {
				Platform: "feishu",
				Engine:   "codex",
				Feishu:   &config.FeishuConfig{AppID: "cli_fs", AppSecret: "fs_secret"},
			},
			"dt": {
				Platform: "dingtalk",
				Engine:   "codex",
				DingTalk: &config.DingTalkConfig{ClientID: "ding_client", ClientSecret: "ding_secret"},
			},
			"tg": {
				Platform: "telegram",
				Engine:   "claude",
				Telegram: &config.TelegramConfig{BotToken: "telegram_token"},
			},
		},
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 3 {
		t.Fatalf("bots = %#v", bots)
	}
	want := map[string]string{"fs": "feishu", "dt": "dingtalk", "tg": "telegram"}
	for _, bot := range bots {
		adapter, name, err := newPlatformAdapter(bot.Config)
		if err != nil {
			t.Fatalf("%s adapter: %v", bot.ID, err)
		}
		if name != want[bot.ID] || adapter.Name() != want[bot.ID] {
			t.Fatalf("%s adapter = %q/%q", bot.ID, name, adapter.Name())
		}
	}
}

type startPlatform struct {
	name    string
	started chan<- string
}

func (p *startPlatform) Name() string { return p.name }
func (p *startPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}
func (p *startPlatform) Start(ctx context.Context, _ func(platform.Message)) error {
	p.started <- p.name
	<-ctx.Done()
	return ctx.Err()
}
func (p *startPlatform) Send(context.Context, platform.Reply) (string, error) {
	return "", nil
}
func (p *startPlatform) Update(context.Context, string, platform.Reply) error { return nil }
func (p *startPlatform) Download(context.Context, attachment.Resource, string) (attachment.Local, error) {
	return attachment.Local{}, nil
}

func TestRunBotRuntimesStartsEveryBot(t *testing.T) {
	started := make(chan string, 2)
	ws, err := execguard.NewWorkspace(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtimes := []*botRuntime{
		{id: "codex", name: "Codex", platform: &startPlatform{name: "codex", started: started}, cfg: config.Config{Bot: config.BotConfig{Name: "Codex"}}, logger: logger, workspaces: ws},
		{id: "claude", name: "Claude", platform: &startPlatform{name: "claude", started: started}, cfg: config.Config{Bot: config.BotConfig{Name: "Claude"}}, logger: logger, workspaces: ws},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runBotRuntimes(ctx, runtimes, logger) }()

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only started %v", seen)
		}
	}
	if !seen["codex"] || !seen["claude"] {
		t.Fatalf("started bots = %v", seen)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runBotRuntimes did not stop")
	}
}
