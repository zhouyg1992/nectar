package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/core/decision"
	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/engine/claude"
	"github.com/zhouyg1992/nectar/internal/engine/codex"
	"github.com/zhouyg1992/nectar/internal/engine/cursor"
	"github.com/zhouyg1992/nectar/internal/engine/opencode"
	"github.com/zhouyg1992/nectar/internal/engine/pi"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/memory/comb"
	"github.com/zhouyg1992/nectar/internal/platform"
	"github.com/zhouyg1992/nectar/internal/platform/dingtalk"
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
	if len(args) > 0 && args[0] == "update" {
		return runUpdate(args[1:])
	}
	if len(args) > 0 && args[0] == "schedule" {
		return runSchedule(args[1:])
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
  nectar update [--check] [--force] [--version vX.Y.Z] [--no-restart]
  nectar schedule <list|add|preview|pause|resume|run|delete|update>
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

	logger := newLogger(platformLogLevel(cfg))
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
		decision.Noop{},
		logger,
		core.RouterConfig{
			AgentID:       cfg.Bot.Engine,
			ModelName:     setup.model,
			ContextWindow: setup.contextWindow,
			EngineOptions: setup.options,
			AdminUsers:    cfg.Bot.AdminUsers,
			DataDir:       cfg.Storage.DataDir,
			APISocket:     filepath.Join(cfg.Storage.DataDir, "run", "api.sock"),
		},
	)

	var scheduler *core.Scheduler
	var scheduleAPI *core.ScheduleAPI
	if cfg.ScheduleEnabled() {
		grace, err := cfg.ScheduleGrace()
		if err != nil {
			return err
		}
		scheduler, err = core.NewScheduler(core.SchedulerOptions{
			StorePath:       filepath.Join(cfg.Storage.DataDir, "schedules.json"),
			Anchors:         anchors,
			DefaultTimezone: cfg.Schedule.Timezone,
			StartupGrace:    grace,
			Logger:          logger,
		})
		if err != nil {
			return fmt.Errorf("load scheduled tasks: %w", err)
		}
		router.SetScheduling(anchors, scheduler)
		scheduleAPI = core.NewScheduleAPI(core.ScheduleAPIOptions{
			SocketPath:     filepath.Join(cfg.Storage.DataDir, "run", "api.sock"),
			Scheduler:      scheduler,
			Logger:         logger,
			DefaultChannel: platformName,
			DefaultAgent:   cfg.Bot.Engine,
			AdminUsers:     cfg.Bot.AdminUsers,
		})
		if err := scheduleAPI.Start(ctx); err != nil {
			return err
		}
		defer scheduleAPI.Close()
		if err := scheduler.Start(ctx, func(task core.ScheduledTask, anchor core.SessionAnchor) error {
			router.Handle(platform.Message{
				MessageID: anchor.MessageID,
				Channel:   anchor.Channel,
				ChatID:    anchor.ChatID,
				ChatType:  anchor.ChatType,
				UserID:    anchor.UserID,
				Text:      task.Prompt,
				ThreadID:  anchor.ThreadID,
				Origin:    "schedule",
			})
			return nil
		}); err != nil {
			return err
		}
		defer scheduler.Stop()
	} else {
		router.SetScheduling(anchors, nil)
	}

	logger.Info("nectar starting",
		"version", version,
		"platform", platformName,
		"engine", setup.name,
		"workspace", workspaces.Root(),
		"data_dir", cfg.Storage.DataDir,
		"model", setup.model,
		"context_window", setup.contextWindow,
		"schedule_enabled", scheduler != nil,
	)
	if platformName == "feishu" {
		logger.Info("connecting to feishu; wait for 'feishu websocket connected' before sending messages")
	} else {
		logger.Info("connecting to dingtalk; wait for 'dingtalk stream connected' before sending messages")
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
