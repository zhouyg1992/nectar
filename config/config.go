package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Feishu   FeishuConfig   `toml:"feishu"`
	Codex    CodexConfig    `toml:"codex"`
	OpenCode OpenCodeConfig `toml:"opencode"`
	Cursor   CursorConfig   `toml:"cursor"`
	Claude   ClaudeConfig   `toml:"claude"`
	Bot      BotConfig      `toml:"bot"`
	Storage  StorageConfig  `toml:"storage"`
}

type FeishuConfig struct {
	AppID     string `toml:"app_id"`
	AppSecret string `toml:"app_secret"`
	LogLevel  string `toml:"log_level"`
}

type CodexConfig struct {
	Path             string `toml:"path"`
	Model            string `toml:"model"`
	Effort           string `toml:"effort"`
	Sandbox          string `toml:"sandbox"`
	WorkingDir       string `toml:"working_dir"`
	Timeout          string `toml:"timeout"`
	MaxOutputBytes   int    `toml:"max_output_bytes"`
	ContextWindow    int    `toml:"context_window"`
	PerChatWorkspace bool   `toml:"per_chat_workspace"`
	BypassApprovals  bool   `toml:"bypass_approvals"`
}

type OpenCodeConfig struct {
	Path             string `toml:"path"`
	Model            string `toml:"model"`
	Agent            string `toml:"agent"`
	WorkingDir       string `toml:"working_dir"`
	Timeout          string `toml:"timeout"`
	MaxOutputBytes   int    `toml:"max_output_bytes"`
	ContextWindow    int    `toml:"context_window"`
	Standalone       bool   `toml:"standalone"`
	AutoApprove      bool   `toml:"auto_approve"`
	PerChatWorkspace bool   `toml:"per_chat_workspace"`
}

type CursorConfig struct {
	Path             string `toml:"path"`
	Model            string `toml:"model"`
	Mode             string `toml:"mode"`
	Sandbox          string `toml:"sandbox"`
	WorkingDir       string `toml:"working_dir"`
	Timeout          string `toml:"timeout"`
	MaxOutputBytes   int    `toml:"max_output_bytes"`
	ContextWindow    int    `toml:"context_window"`
	AutoApprove      bool   `toml:"auto_approve"`
	TrustWorkspace   bool   `toml:"trust_workspace"`
	PerChatWorkspace bool   `toml:"per_chat_workspace"`
}

type ClaudeConfig struct {
	Path             string `toml:"path"`
	Model            string `toml:"model"`
	FallbackModel    string `toml:"fallback_model"`
	PermissionMode   string `toml:"permission_mode"`
	WorkingDir       string `toml:"working_dir"`
	Timeout          string `toml:"timeout"`
	MaxOutputBytes   int    `toml:"max_output_bytes"`
	ContextWindow    int    `toml:"context_window"`
	MaxTurns         int    `toml:"max_turns"`
	AutoApprove      bool   `toml:"auto_approve"`
	PerChatWorkspace bool   `toml:"per_chat_workspace"`
}

type BotConfig struct {
	Name         string   `toml:"name"`
	Engine       string   `toml:"engine"`
	AgentID      string   `toml:"agent_id"`
	AdminUsers   []string `toml:"admin_users"`
	AllowedChats []string `toml:"allowed_chats"`
}

type StorageConfig struct {
	DataDir string `toml:"data_dir"`
}

func defaults() Config {
	home := homeDir()
	return Config{
		Feishu: FeishuConfig{LogLevel: "info"},
		Codex: CodexConfig{
			Path:           "codex",
			Sandbox:        "workspace-write",
			WorkingDir:     filepath.Join(home, ".nectar", "workspace"),
			Timeout:        "30m",
			MaxOutputBytes: 2 << 20,
		},
		OpenCode: OpenCodeConfig{
			Path:           "opencode",
			WorkingDir:     filepath.Join(home, ".nectar", "workspace"),
			Timeout:        "30m",
			MaxOutputBytes: 2 << 20,
			Standalone:     true,
			AutoApprove:    true,
		},
		Cursor: CursorConfig{
			Path:           "cursor-agent",
			WorkingDir:     filepath.Join(home, ".nectar", "workspace"),
			Timeout:        "30m",
			MaxOutputBytes: 2 << 20,
			AutoApprove:    true,
			TrustWorkspace: true,
		},
		Claude: ClaudeConfig{
			Path:           "claude",
			WorkingDir:     filepath.Join(home, ".nectar", "workspace"),
			Timeout:        "30m",
			MaxOutputBytes: 2 << 20,
			AutoApprove:    true,
		},
		Bot:     BotConfig{Name: "Nectar", AgentID: "codex"},
		Storage: StorageConfig{DataDir: filepath.Join(home, ".nectar")},
	}
}

func DefaultPath() string {
	return filepath.Join(homeDir(), ".nectar", "config.toml")
}

func Load(path string) (Config, error) {
	cfg := defaults()
	if path == "" {
		path = DefaultPath()
	}

	expanded, err := expandHome(path)
	if err != nil {
		return Config{}, err
	}
	if _, err := os.Stat(expanded); err == nil {
		if _, err := toml.DecodeFile(expanded, &cfg); err != nil {
			return Config{}, fmt.Errorf("decode config %s: %w", expanded, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("stat config %s: %w", expanded, err)
	}

	applyEnv(&cfg)
	if err := normalize(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Timeout() (time.Duration, error) {
	return parseTimeout("codex.timeout", c.Codex.Timeout)
}

func (c Config) OpenCodeTimeout() (time.Duration, error) {
	return parseTimeout("opencode.timeout", c.OpenCode.Timeout)
}

func (c Config) CursorTimeout() (time.Duration, error) {
	return parseTimeout("cursor.timeout", c.Cursor.Timeout)
}

func (c Config) ClaudeTimeout() (time.Duration, error) {
	return parseTimeout("claude.timeout", c.Claude.Timeout)
}

func parseTimeout(name, value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, value, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return d, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("FEISHU_APP_ID"); v != "" {
		cfg.Feishu.AppID = v
	}
	if v := os.Getenv("FEISHU_APP_SECRET"); v != "" {
		cfg.Feishu.AppSecret = v
	}
	if v := os.Getenv("CODEX_PATH"); v != "" {
		cfg.Codex.Path = v
	}
	if v := os.Getenv("OPENCODE_PATH"); v != "" {
		cfg.OpenCode.Path = v
	}
	if v := os.Getenv("CURSOR_AGENT_PATH"); v != "" {
		cfg.Cursor.Path = v
	}
	if v := os.Getenv("CLAUDE_PATH"); v != "" {
		cfg.Claude.Path = v
	}
	if v := os.Getenv("NECTAR_ENGINE"); v != "" {
		cfg.Bot.Engine = v
	}
	if v := os.Getenv("NECTAR_DATA_DIR"); v != "" {
		cfg.Storage.DataDir = v
	}
	if v := os.Getenv("NECTAR_WORKING_DIR"); v != "" {
		cfg.Codex.WorkingDir = v
		cfg.OpenCode.WorkingDir = v
		cfg.Cursor.WorkingDir = v
		cfg.Claude.WorkingDir = v
	}
}

func normalize(cfg *Config) error {
	var err error
	if cfg.Storage.DataDir, err = expandHome(cfg.Storage.DataDir); err != nil {
		return err
	}
	if cfg.Codex.WorkingDir, err = expandHome(cfg.Codex.WorkingDir); err != nil {
		return err
	}
	if cfg.OpenCode.WorkingDir, err = expandHome(cfg.OpenCode.WorkingDir); err != nil {
		return err
	}
	if cfg.Cursor.WorkingDir, err = expandHome(cfg.Cursor.WorkingDir); err != nil {
		return err
	}
	if cfg.Claude.WorkingDir, err = expandHome(cfg.Claude.WorkingDir); err != nil {
		return err
	}
	if cfg.Storage.DataDir == "" {
		return errors.New("storage.data_dir is required")
	}
	if cfg.Codex.WorkingDir == "" {
		return errors.New("codex.working_dir is required")
	}
	if cfg.OpenCode.WorkingDir == "" {
		return errors.New("opencode.working_dir is required")
	}
	if cfg.Cursor.WorkingDir == "" {
		return errors.New("cursor.working_dir is required")
	}
	if cfg.Claude.WorkingDir == "" {
		return errors.New("claude.working_dir is required")
	}
	if strings.TrimSpace(cfg.Codex.Path) == "" {
		cfg.Codex.Path = "codex"
	}
	if strings.TrimSpace(cfg.OpenCode.Path) == "" {
		cfg.OpenCode.Path = "opencode"
	}
	if strings.TrimSpace(cfg.Cursor.Path) == "" {
		cfg.Cursor.Path = "cursor-agent"
	}
	if strings.TrimSpace(cfg.Claude.Path) == "" {
		cfg.Claude.Path = "claude"
	}
	switch mode := strings.TrimSpace(cfg.Cursor.Mode); mode {
	case "", "plan", "ask":
		cfg.Cursor.Mode = mode
	default:
		return fmt.Errorf("unsupported cursor.mode %q (supported: plan, ask)", mode)
	}
	switch sandbox := strings.TrimSpace(cfg.Cursor.Sandbox); sandbox {
	case "", "enabled", "disabled":
		cfg.Cursor.Sandbox = sandbox
	default:
		return fmt.Errorf("unsupported cursor.sandbox %q (supported: enabled, disabled)", sandbox)
	}
	switch mode := strings.TrimSpace(cfg.Claude.PermissionMode); mode {
	case "", "acceptEdits", "auto", "bypassPermissions", "manual", "dontAsk", "plan":
		cfg.Claude.PermissionMode = mode
	default:
		return fmt.Errorf("unsupported claude.permission_mode %q", mode)
	}
	if cfg.Claude.MaxTurns < 0 {
		return errors.New("claude.max_turns must not be negative")
	}
	if strings.TrimSpace(cfg.Bot.Name) == "" {
		cfg.Bot.Name = "Nectar"
	}
	engine := strings.TrimSpace(cfg.Bot.Engine)
	if engine == "" {
		engine = strings.TrimSpace(cfg.Bot.AgentID)
	}
	if engine == "" {
		engine = "codex"
	}
	switch engine {
	case "codex", "opencode", "cursor", "claude":
	default:
		return fmt.Errorf("unsupported bot.engine %q (supported: codex, opencode, cursor, claude)", engine)
	}
	cfg.Bot.Engine = engine
	cfg.Bot.AgentID = engine
	if cfg.Codex.MaxOutputBytes <= 0 {
		return errors.New("codex.max_output_bytes must be positive")
	}
	if cfg.OpenCode.MaxOutputBytes <= 0 {
		return errors.New("opencode.max_output_bytes must be positive")
	}
	if cfg.Cursor.MaxOutputBytes <= 0 {
		return errors.New("cursor.max_output_bytes must be positive")
	}
	if cfg.Claude.MaxOutputBytes <= 0 {
		return errors.New("claude.max_output_bytes must be positive")
	}
	if _, err := cfg.Timeout(); err != nil {
		return err
	}
	if _, err := cfg.OpenCodeTimeout(); err != nil {
		return err
	}
	if _, err := cfg.CursorTimeout(); err != nil {
		return err
	}
	if _, err := cfg.ClaudeTimeout(); err != nil {
		return err
	}
	return nil
}

func expandHome(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if path == "~" {
		return homeDir(), nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), path[2:]), nil
	}
	return path, nil
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
