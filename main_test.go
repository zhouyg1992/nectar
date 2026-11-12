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
