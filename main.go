package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/core/decision"
	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/engine/codex"
	"github.com/zhouyg1992/nectar/internal/engine/cursor"
	"github.com/zhouyg1992/nectar/internal/engine/opencode"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/memory/comb"
	"github.com/zhouyg1992/nectar/internal/platform/feishu"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "nectar:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "service" {
		return runService(args[1:])
	}
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		printMainUsage()
		return nil
	}

	fs := flag.NewFlagSet("nectar", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *showVersion {
		fmt.Println("nectar", version)
		return nil
	}

	return runBot(*configPath)
}

func printMainUsage() {
	fmt.Print(`Usage:
  nectar [-config PATH]
  nectar -version
  nectar service <install|uninstall|start|stop|restart|status|logs>
`)
}

func runBot(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	setup, err := setupEngine(cfg)
	if err != nil {
		return err
	}

	logger := newLogger(cfg.Feishu.LogLevel)
	slog.SetDefault(logger)

	if err := os.MkdirAll(cfg.Storage.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	lessons, err := comb.NewFileStore(cfg.Storage.DataDir)
	if err != nil {
		return err
	}
	workspaces, err := execguard.NewWorkspace(setup.workspaceDir, setup.perChatWorkspace)
	if err != nil {
		return err
	}

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
		setup.adapter,
		core.NewSessionStore(),
		core.NewQueue(),
		workspaces,
		lessons,
		decision.Noop{},
		logger,
		core.RouterConfig{
			AgentID:       cfg.Bot.AgentID,
			ModelName:     setup.model,
			ContextWindow: setup.contextWindow,
			EngineOptions: setup.options,
		},
	)

	logger.Info("nectar starting",
		"version", version,
		"platform", "feishu",
		"engine", setup.name,
		"workspace", workspaces.Root(),
		"data_dir", cfg.Storage.DataDir,
		"model", setup.model,
		"context_window", setup.contextWindow,
	)
	logger.Info("connecting to feishu; wait for 'feishu websocket connected' before sending messages")
	if setup.name == "codex" && setup.contextWindow == 0 {
		logger.Warn("codex context window unknown; card footer will omit context usage (set codex.context_window to override)")
	}
	if len(cfg.Bot.AdminUsers) == 0 && len(cfg.Bot.AllowedChats) == 0 {
		logger.Warn("no user/chat allowlist configured; anyone outside Feishu app scope who can reach the bot can run the configured agent")
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

type engineSetup struct {
	adapter          engine.EngineAdapter
	options          engine.EngineOptions
	name             string
	model            string
	contextWindow    int
	workspaceDir     string
	perChatWorkspace bool
}

func setupEngine(cfg config.Config) (engineSetup, error) {
	switch cfg.Bot.Engine {
	case "codex":
		timeout, err := cfg.Timeout()
		if err != nil {
			return engineSetup{}, err
		}
		return engineSetup{
			adapter: codex.NewAdapter(codex.Config{
				Executable:      cfg.Codex.Path,
				Model:           cfg.Codex.Model,
				Effort:          cfg.Codex.Effort,
				Sandbox:         cfg.Codex.Sandbox,
				WorkingDir:      cfg.Codex.WorkingDir,
				Timeout:         timeout,
				MaxOutputBytes:  cfg.Codex.MaxOutputBytes,
				BypassApprovals: cfg.Codex.BypassApprovals,
			}),
			options: engine.EngineOptions{
				Model:          cfg.Codex.Model,
				Effort:         cfg.Codex.Effort,
				Sandbox:        cfg.Codex.Sandbox,
				WorkingDir:     cfg.Codex.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.Codex.MaxOutputBytes,
			},
			name:             "codex",
			model:            codex.ResolveModel(cfg.Codex.Model),
			contextWindow:    codex.ResolveContextWindow(cfg.Codex.Model, cfg.Codex.ContextWindow),
			workspaceDir:     cfg.Codex.WorkingDir,
			perChatWorkspace: cfg.Codex.PerChatWorkspace,
		}, nil
	case "opencode":
		timeout, err := cfg.OpenCodeTimeout()
		if err != nil {
			return engineSetup{}, err
		}
		model := opencode.ResolveModel(cfg.OpenCode.Model)
		if model == "" {
			model = "opencode"
		}
		return engineSetup{
			adapter: opencode.NewAdapter(opencode.Config{
				Executable:     cfg.OpenCode.Path,
				Model:          cfg.OpenCode.Model,
				Agent:          cfg.OpenCode.Agent,
				WorkingDir:     cfg.OpenCode.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.OpenCode.MaxOutputBytes,
				Standalone:     cfg.OpenCode.Standalone,
				AutoApprove:    cfg.OpenCode.AutoApprove,
			}),
			options: engine.EngineOptions{
				Model:          cfg.OpenCode.Model,
				WorkingDir:     cfg.OpenCode.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.OpenCode.MaxOutputBytes,
			},
			name:             "opencode",
			model:            model,
			contextWindow:    opencode.ResolveContextWindow(cfg.OpenCode.Model, cfg.OpenCode.ContextWindow),
			workspaceDir:     cfg.OpenCode.WorkingDir,
			perChatWorkspace: cfg.OpenCode.PerChatWorkspace,
		}, nil
	case "cursor":
		timeout, err := cfg.CursorTimeout()
		if err != nil {
			return engineSetup{}, err
		}
		model := strings.TrimSpace(cfg.Cursor.Model)
		if model == "" {
			model = "cursor"
		}
		return engineSetup{
			adapter: cursor.NewAdapter(cursor.Config{
				Executable:     cfg.Cursor.Path,
				Model:          cfg.Cursor.Model,
				Mode:           cfg.Cursor.Mode,
				Sandbox:        cfg.Cursor.Sandbox,
				WorkingDir:     cfg.Cursor.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.Cursor.MaxOutputBytes,
				AutoApprove:    cfg.Cursor.AutoApprove,
				TrustWorkspace: cfg.Cursor.TrustWorkspace,
			}),
			options: engine.EngineOptions{
				Model:          cfg.Cursor.Model,
				Sandbox:        cfg.Cursor.Sandbox,
				WorkingDir:     cfg.Cursor.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.Cursor.MaxOutputBytes,
			},
			name:             "cursor",
			model:            model,
			contextWindow:    cfg.Cursor.ContextWindow,
			workspaceDir:     cfg.Cursor.WorkingDir,
			perChatWorkspace: cfg.Cursor.PerChatWorkspace,
		}, nil
	default:
		return engineSetup{}, fmt.Errorf("unsupported engine %q", cfg.Bot.Engine)
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
