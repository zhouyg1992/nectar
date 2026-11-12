package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/core/decision"
	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/engine/codex"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/memory/comb"
	"github.com/zhouyg1992/nectar/internal/platform/feishu"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nectar:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to config.toml")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("nectar", version)
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	timeout, err := cfg.Timeout()
	if err != nil {
		return err
	}
	contextWindow := codex.ResolveContextWindow(cfg.Codex.Model, cfg.Codex.ContextWindow)
	effectiveModel := codex.ResolveModel(cfg.Codex.Model)

	logger := newLogger(cfg.Feishu.LogLevel)
	slog.SetDefault(logger)

	if err := os.MkdirAll(cfg.Storage.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	lessons, err := comb.NewFileStore(cfg.Storage.DataDir)
	if err != nil {
		return err
	}
	workspaces, err := execguard.NewWorkspace(cfg.Codex.WorkingDir, cfg.Codex.PerChatWorkspace)
	if err != nil {
		return err
	}

	codexEngine := codex.NewAdapter(codex.Config{
		Executable:      cfg.Codex.Path,
		Model:           cfg.Codex.Model,
		Effort:          cfg.Codex.Effort,
		Sandbox:         cfg.Codex.Sandbox,
		WorkingDir:      cfg.Codex.WorkingDir,
		Timeout:         timeout,
		MaxOutputBytes:  cfg.Codex.MaxOutputBytes,
		BypassApprovals: cfg.Codex.BypassApprovals,
	})
	platformAdapter, err := feishu.New(feishu.Config{
		AppID:        cfg.Feishu.AppID,
		AppSecret:    cfg.Feishu.AppSecret,
		LogLevel:     cfg.Feishu.LogLevel,
		AllowedUsers: cfg.Bot.AdminUsers,
		AllowedChats: cfg.Bot.AllowedChats,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	router := core.NewRouter(
		ctx,
		platformAdapter,
		codexEngine,
		core.NewSessionStore(),
		core.NewQueue(),
		workspaces,
		lessons,
		decision.Noop{},
		logger,
		core.RouterConfig{
			AgentID:       cfg.Bot.AgentID,
			ModelName:     effectiveModel,
			ContextWindow: contextWindow,
			EngineOptions: engine.EngineOptions{
				Model:          cfg.Codex.Model,
				Effort:         cfg.Codex.Effort,
				Sandbox:        cfg.Codex.Sandbox,
				WorkingDir:     cfg.Codex.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.Codex.MaxOutputBytes,
			},
		},
	)

	logger.Info("nectar starting",
		"version", version,
		"platform", "feishu",
		"engine", "codex",
		"workspace", workspaces.Root(),
		"data_dir", cfg.Storage.DataDir,
		"model", effectiveModel,
		"context_window", contextWindow,
	)
	logger.Info("connecting to feishu; wait for 'feishu websocket connected' before sending messages")
	if contextWindow == 0 {
		logger.Warn("codex context window unknown; card footer will omit context usage (set codex.context_window to override)")
	}
	if len(cfg.Bot.AdminUsers) == 0 && len(cfg.Bot.AllowedChats) == 0 {
		logger.Warn("no user/chat allowlist configured; anyone outside Feishu app scope who can reach the bot can run Codex")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- platformAdapter.Start(ctx, router.Handle)
	}()

	select {
	case <-ctx.Done():
		logger.Info("nectar shutting down")
		err := <-errCh
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
