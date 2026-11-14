package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/cli"
	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/core/decision"
	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/engine/claude"
	"github.com/zhouyg1992/nectar/internal/engine/codex"
	"github.com/zhouyg1992/nectar/internal/engine/cursor"
	"github.com/zhouyg1992/nectar/internal/engine/opencode"
	"github.com/zhouyg1992/nectar/internal/engine/pi"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/memory"
	"github.com/zhouyg1992/nectar/internal/memory/comb"
	"github.com/zhouyg1992/nectar/internal/platform"
	"github.com/zhouyg1992/nectar/internal/platform/dingtalk"
	"github.com/zhouyg1992/nectar/internal/platform/feishu"
	"github.com/zhouyg1992/nectar/internal/platform/telegram"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "nectar:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return cli.Run(args, cli.Deps{
		Version: version,
		RunBot:  runBot,
	})
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

	logger := newLogger(platformLogLevel(cfg))
	slog.SetDefault(logger)

	if err := os.MkdirAll(cfg.Storage.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	lessons, err := comb.NewFileStore(cfg.Storage.DataDir)
	if err != nil {
		return err
	}
	var memories memory.Store
	if cfg.MemoryEnabled() {
		store, err := memory.NewFileStore(cfg.Storage.DataDir)
		if err != nil {
			return err
		}
		memories = store
	}
	workspaces, err := execguard.NewWorkspace(setup.workspaceDir, setup.perChatWorkspace)
	if err != nil {
		return err
	}

	platformAdapter, platformName, err := newPlatformAdapter(cfg)
	if err != nil {
		return err
	}

	sessions, err := core.NewPersistentSessionStore(filepath.Join(cfg.Storage.DataDir, "sessions.json"))
	if err != nil {
		logger.Warn("could not load persisted sessions; starting with empty sessions", "error", err)
		sessions = core.NewSessionStore()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	anchors, err := core.NewAnchorStore(cfg.Storage.DataDir)
	if err != nil {
		return fmt.Errorf("load delivery anchors: %w", err)
	}

	router := core.NewRouter(
		ctx,
		platformAdapter,
		setup.adapter,
		sessions,
		core.NewQueue(),
		workspaces,
		lessons,
		memories,
		decision.Noop{},
		logger,
		core.RouterConfig{
			AgentID:                cfg.Bot.Engine,
			BotName:                cfg.Bot.Name,
			Version:                version,
			ModelName:              setup.model,
			ContextWindow:          setup.contextWindow,
			EngineOptions:          setup.options,
			AdminUsers:             cfg.Bot.AdminUsers,
			DataDir:                cfg.Storage.DataDir,
			APISocket:              filepath.Join(cfg.Storage.DataDir, "run", "api.sock"),
			MemoryMaxItems:         cfg.Memory.MaxItems,
			MemoryMaxChars:         cfg.Memory.MaxChars,
			DisableMemoryAutoLearn: !cfg.MemoryAutoLearnFailures(),
		},
	)

	var scheduler *core.Scheduler
	var scheduleAPI *core.ScheduleAPI
	if cfg.ScheduleEnabled() {
		grace, err := cfg.ScheduleGrace()
		if err != nil {
			return err
		}
		jitter, err := cfg.ScheduleJitter()
		if err != nil {
			return err
		}
		timeout, err := cfg.ScheduleTimeout()
		if err != nil {
			return err
		}
		scheduler, err = core.NewScheduler(core.SchedulerOptions{
			StorePath:       filepath.Join(cfg.Storage.DataDir, "schedules.json"),
			Anchors:         anchors,
			DefaultTimezone: cfg.Schedule.Timezone,
			StartupGrace:    grace,
			HistoryLimit:    cfg.Schedule.HistoryLimit,
			MaxConcurrent:   cfg.Schedule.MaxConcurrent,
			Jitter:          jitter,
			DefaultTimeout:  timeout,
			Logger:          logger,
		})
		if err != nil {
			return fmt.Errorf("load scheduled tasks: %w", err)
		}
		router.SetScheduling(anchors, scheduler)
		if err := scheduler.Start(ctx, func(runCtx context.Context, task core.ScheduledTask, anchor core.SessionAnchor) error {
			return router.ExecuteScheduled(runCtx, platform.Message{
				MessageID: anchor.MessageID,
				Channel:   anchor.Channel,
				ChatID:    anchor.ChatID,
				ChatType:  anchor.ChatType,
				UserID:    anchor.UserID,
				Text:      task.Prompt,
				ThreadID:  anchor.ThreadID,
				Origin:    "schedule",
			})
		}); err != nil {
			return err
		}
		defer scheduler.Stop()
	} else {
		router.SetScheduling(anchors, nil)
	}
	if scheduler != nil || memories != nil {
		scheduleAPI = core.NewScheduleAPI(core.ScheduleAPIOptions{
			SocketPath:     filepath.Join(cfg.Storage.DataDir, "run", "api.sock"),
			Scheduler:      scheduler,
			Memory:         memories,
			Logger:         logger,
			DefaultChannel: platformName,
			DefaultAgent:   cfg.Bot.Engine,
			AdminUsers:     cfg.Bot.AdminUsers,
		})
		if err := scheduleAPI.Start(ctx); err != nil {
			return err
		}
		defer scheduleAPI.Close()
	}

	logger.Info("nectar starting",
		"version", version,
		"platform", platformName,
		"engine", setup.name,
		"workspace", workspaces.Root(),
		"data_dir", cfg.Storage.DataDir,
		"model", setup.model,
		"context_window", setup.contextWindow,
		"memory_enabled", memories != nil,
		"schedule_enabled", scheduler != nil,
	)
	switch platformName {
	case "feishu":
		logger.Info("connecting to feishu; wait for 'feishu websocket connected' before sending messages")
	case "dingtalk":
		logger.Info("connecting to dingtalk; wait for 'dingtalk stream connected' before sending messages")
	case "telegram":
		logger.Info("connecting to telegram; wait for 'telegram: connected' before sending messages")
	}
	if setup.name == "codex" && setup.contextWindow == 0 {
		logger.Warn("codex context window unknown; card footer will omit context usage (set codex.context_window to override)")
	}
	if len(cfg.Bot.AdminUsers) == 0 && len(cfg.Bot.AllowedChats) == 0 {
		logger.Warn("no user/chat allowlist configured; anyone who can reach the bot may run the configured agent")
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

func newPlatformAdapter(cfg config.Config) (platform.PlatformAdapter, string, error) {
	switch cfg.Bot.Platform {
	case "dingtalk":
		adapter, err := dingtalk.New(dingtalk.Config{
			ClientID:     cfg.DingTalk.ClientID,
			ClientSecret: cfg.DingTalk.ClientSecret,
			RobotCode:    cfg.DingTalk.RobotCode,
			LogLevel:     cfg.DingTalk.LogLevel,
			AllowedUsers: cfg.Bot.AdminUsers,
			AllowedChats: cfg.Bot.AllowedChats,
			CardEnabled:  cfg.DingTalk.CardEnabled,
		})
		if err != nil {
			return nil, "", err
		}
		return adapter, "dingtalk", nil
	case "telegram":
		adapter, err := telegram.New(telegram.Config{
			BotToken:     cfg.Telegram.BotToken,
			Proxy:        cfg.Telegram.Proxy,
			APIBaseURL:   cfg.Telegram.APIBaseURL,
			AllowedUsers: cfg.Bot.AdminUsers,
			AllowedChats: cfg.Bot.AllowedChats,
		})
		if err != nil {
			return nil, "", err
		}
		return adapter, "telegram", nil
	case "feishu", "":
		adapter, err := feishu.New(feishu.Config{
			AppID:        cfg.Feishu.AppID,
			AppSecret:    cfg.Feishu.AppSecret,
			LogLevel:     cfg.Feishu.LogLevel,
			AllowedUsers: cfg.Bot.AdminUsers,
			AllowedChats: cfg.Bot.AllowedChats,
		})
		if err != nil {
			return nil, "", err
		}
		return adapter, "feishu", nil
	default:
		return nil, "", fmt.Errorf("unsupported bot.platform %q", cfg.Bot.Platform)
	}
}

func platformLogLevel(cfg config.Config) string {
	if cfg.Bot.Platform == "dingtalk" {
		return cfg.DingTalk.LogLevel
	}
	if cfg.Bot.Platform == "telegram" {
		return cfg.Telegram.LogLevel
	}
	return cfg.Feishu.LogLevel
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
	case "claude":
		timeout, err := cfg.ClaudeTimeout()
		if err != nil {
			return engineSetup{}, err
		}
		model := claude.ResolveModel(cfg.Claude.Model)
		if model == "" {
			model = "claude"
		}
		return engineSetup{
			adapter: claude.NewAdapter(claude.Config{
				Executable:     cfg.Claude.Path,
				Model:          cfg.Claude.Model,
				FallbackModel:  cfg.Claude.FallbackModel,
				PermissionMode: cfg.Claude.PermissionMode,
				WorkingDir:     cfg.Claude.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.Claude.MaxOutputBytes,
				MaxTurns:       cfg.Claude.MaxTurns,
				AutoApprove:    cfg.Claude.AutoApprove,
			}),
			options: engine.EngineOptions{
				Model:          cfg.Claude.Model,
				WorkingDir:     cfg.Claude.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.Claude.MaxOutputBytes,
			},
			name:             "claude",
			model:            model,
			contextWindow:    cfg.Claude.ContextWindow,
			workspaceDir:     cfg.Claude.WorkingDir,
			perChatWorkspace: cfg.Claude.PerChatWorkspace,
		}, nil
	case "pi":
		timeout, err := cfg.PiTimeout()
		if err != nil {
			return engineSetup{}, err
		}
		model := pi.ResolveModel(cfg.Pi.Model)
		if model == "" {
			model = "pi"
		}
		return engineSetup{
			adapter: pi.NewAdapter(pi.Config{
				Executable:     cfg.Pi.Path,
				Provider:       cfg.Pi.Provider,
				Model:          cfg.Pi.Model,
				Thinking:       cfg.Pi.Thinking,
				WorkingDir:     cfg.Pi.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.Pi.MaxOutputBytes,
				TrustWorkspace: cfg.Pi.TrustWorkspace,
			}),
			options: engine.EngineOptions{
				Model:          cfg.Pi.Model,
				WorkingDir:     cfg.Pi.WorkingDir,
				Timeout:        timeout,
				MaxOutputBytes: cfg.Pi.MaxOutputBytes,
			},
			name:             "pi",
			model:            model,
			contextWindow:    cfg.Pi.ContextWindow,
			workspaceDir:     cfg.Pi.WorkingDir,
			perChatWorkspace: cfg.Pi.PerChatWorkspace,
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
