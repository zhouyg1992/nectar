package main

import (
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/config"
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
