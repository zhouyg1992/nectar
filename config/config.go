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
	DingTalk DingTalkConfig `toml:"dingtalk"`
	Codex    CodexConfig    `toml:"codex"`
	OpenCode OpenCodeConfig `toml:"opencode"`
	Cursor   CursorConfig   `toml:"cursor"`
	Claude   ClaudeConfig   `toml:"claude"`
	Pi       PiConfig       `toml:"pi"`
	Bot      BotConfig      `toml:"bot"`
	Storage  StorageConfig  `toml:"storage"`
	Schedule ScheduleConfig `toml:"schedule"`
}

type FeishuConfig struct {
	AppID     string `toml:"app_id"`
	AppSecret string `toml:"app_secret"`
	LogLevel  string `toml:"log_level"`
}

type DingTalkConfig struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	AppKey       string `toml:"app_key"`
	AppSecret    string `toml:"app_secret"`
	RobotCode    string `toml:"robot_code"`
	LogLevel     string `toml:"log_level"`
	CardEnabled  *bool  `toml:"card_enabled"`
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

type PiConfig struct {
	Path             string `toml:"path"`
	Provider         string `toml:"provider"`
	Model            string `toml:"model"`
	Thinking         string `toml:"thinking"`
	WorkingDir       string `toml:"working_dir"`
	Timeout          string `toml:"timeout"`
	MaxOutputBytes   int    `toml:"max_output_bytes"`
	ContextWindow    int    `toml:"context_window"`
	TrustWorkspace   bool   `toml:"trust_workspace"`
	PerChatWorkspace bool   `toml:"per_chat_workspace"`
}

type BotConfig struct {
	Name         string   `toml:"name"`
	Engine       string   `toml:"engine"`
	Platform     string   `toml:"platform"`
	AdminUsers   []string `toml:"admin_users"`
	AllowedChats []string `toml:"allowed_chats"`
}

type StorageConfig struct {
	DataDir string `toml:"data_dir"`
}

type ScheduleConfig struct {
	Enabled      *bool  `toml:"enabled"`
	Timezone     string `toml:"timezone"`
	StartupGrace string `toml:"startup_grace"`
	HistoryLimit int    `toml:"history_limit"`
}

func defaults() Config {
	home := homeDir()
	return Config{
		Feishu:   FeishuConfig{LogLevel: "info"},
		DingTalk: DingTalkConfig{LogLevel: "info"},
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
			Path:           "agent",
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
		Pi: PiConfig{
			Path:           "pi",
			WorkingDir:     filepath.Join(home, ".nectar", "workspace"),
			Timeout:        "30m",
			MaxOutputBytes: 2 << 20,
			TrustWorkspace: true,
		},
		Bot:     BotConfig{Name: "Nectar"},
		Storage: StorageConfig{DataDir: filepath.Join(home, ".nectar")},
		Schedule: ScheduleConfig{
			StartupGrace: "5m",
			HistoryLimit: 100,
		},
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

func (c Config) PiTimeout() (time.Duration, error) {
	return parseTimeout("pi.timeout", c.Pi.Timeout)
}

func (c Config) ScheduleEnabled() bool {
	return c.Schedule.Enabled == nil || *c.Schedule.Enabled
}

func (c Config) ScheduleGrace() (time.Duration, error) {
	value := strings.TrimSpace(c.Schedule.StartupGrace)
	if value == "" {
		return 5 * time.Minute, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid schedule.startup_grace %q: %w", value, err)
	}
	if d < 0 {
		return 0, errors.New("schedule.startup_grace must not be negative")
	}
	return d, nil
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
	if v := os.Getenv("DINGTALK_CLIENT_ID"); v != "" {
		cfg.DingTalk.ClientID = v
	}
	if v := os.Getenv("DINGTALK_CLIENT_SECRET"); v != "" {
		cfg.DingTalk.ClientSecret = v
	}
	if v := os.Getenv("DINGTALK_ROBOT_CODE"); v != "" {
		cfg.DingTalk.RobotCode = v
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
	if v := os.Getenv("PI_PATH"); v != "" {
		cfg.Pi.Path = v
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
		cfg.Pi.WorkingDir = v
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
	if cfg.Pi.WorkingDir, err = expandHome(cfg.Pi.WorkingDir); err != nil {
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
	if cfg.Pi.WorkingDir == "" {
		return errors.New("pi.working_dir is required")
	}
	if strings.TrimSpace(cfg.Codex.Path) == "" {
		cfg.Codex.Path = "codex"
	}
	if strings.TrimSpace(cfg.OpenCode.Path) == "" {
		cfg.OpenCode.Path = "opencode"
	}
	if strings.TrimSpace(cfg.Cursor.Path) == "" {
		cfg.Cursor.Path = "agent"
	}
	if strings.TrimSpace(cfg.Claude.Path) == "" {
		cfg.Claude.Path = "claude"
	}
	if strings.TrimSpace(cfg.Pi.Path) == "" {
		cfg.Pi.Path = "pi"
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
	switch thinking := strings.TrimSpace(cfg.Pi.Thinking); thinking {
	case "", "off", "minimal", "low", "medium", "high", "xhigh", "max":
		cfg.Pi.Thinking = thinking
	default:
		return fmt.Errorf("unsupported pi.thinking %q (supported: off, minimal, low, medium, high, xhigh, max)", thinking)
	}
	if strings.TrimSpace(cfg.Bot.Name) == "" {
		cfg.Bot.Name = "Nectar"
	}
	cfg.Schedule.Timezone = strings.TrimSpace(cfg.Schedule.Timezone)
	if cfg.Schedule.Timezone == "" {
		cfg.Schedule.Timezone = "Local"
	}
	if !strings.EqualFold(cfg.Schedule.Timezone, "Local") {
		if _, err := time.LoadLocation(cfg.Schedule.Timezone); err != nil {
			return fmt.Errorf("invalid schedule.timezone %q: %w", cfg.Schedule.Timezone, err)
		}
	}
	if _, err := cfg.ScheduleGrace(); err != nil {
		return err
	}
	if cfg.Schedule.HistoryLimit < 0 {
		return errors.New("schedule.history_limit must not be negative")
	}
	if err := normalizePlatform(cfg); err != nil {
		return err
	}
	engine := strings.TrimSpace(cfg.Bot.Engine)
	if engine == "" {
		engine = "codex"
	}
	switch engine {
	case "codex", "opencode", "cursor", "claude", "pi":
	default:
		return fmt.Errorf("unsupported bot.engine %q (supported: codex, opencode, cursor, claude, pi)", engine)
	}
	cfg.Bot.Engine = engine
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
	if cfg.Pi.MaxOutputBytes <= 0 {
		return errors.New("pi.max_output_bytes must be positive")
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
	if _, err := cfg.PiTimeout(); err != nil {
		return err
	}
	return nil
}

func normalizePlatform(cfg *Config) error {
	cfg.DingTalk.ClientID = strings.TrimSpace(cfg.DingTalk.ClientID)
	cfg.DingTalk.ClientSecret = strings.TrimSpace(cfg.DingTalk.ClientSecret)
	cfg.DingTalk.AppKey = strings.TrimSpace(cfg.DingTalk.AppKey)
	cfg.DingTalk.AppSecret = strings.TrimSpace(cfg.DingTalk.AppSecret)
	cfg.DingTalk.RobotCode = strings.TrimSpace(cfg.DingTalk.RobotCode)
	cfg.DingTalk.LogLevel = strings.ToLower(strings.TrimSpace(cfg.DingTalk.LogLevel))
	if cfg.DingTalk.ClientID == "" {
		cfg.DingTalk.ClientID = cfg.DingTalk.AppKey
	}
	if cfg.DingTalk.ClientSecret == "" {
		cfg.DingTalk.ClientSecret = cfg.DingTalk.AppSecret
	}
	if cfg.DingTalk.RobotCode == "" {
		cfg.DingTalk.RobotCode = cfg.DingTalk.ClientID
	}
	if cfg.DingTalk.LogLevel == "" {
		cfg.DingTalk.LogLevel = "info"
	}

	platform := strings.ToLower(strings.TrimSpace(cfg.Bot.Platform))
	dingtalkConfigured := credentialConfigured(cfg.DingTalk.ClientID) || credentialConfigured(cfg.DingTalk.ClientSecret)
	feishuConfigured := credentialConfigured(cfg.Feishu.AppID) || credentialConfigured(cfg.Feishu.AppSecret)

	switch platform {
	case "":
		switch {
		case dingtalkConfigured && feishuConfigured:
			return errors.New("both [feishu] and [dingtalk] credentials are configured; set bot.platform to feishu or dingtalk")
		case dingtalkConfigured:
			platform = "dingtalk"
		default:
			platform = "feishu"
		}
	case "feishu", "dingtalk":
	default:
		return fmt.Errorf("unsupported bot.platform %q (supported: feishu, dingtalk)", platform)
	}

	if platform == "dingtalk" {
		if !credentialConfigured(cfg.DingTalk.ClientID) || !credentialConfigured(cfg.DingTalk.ClientSecret) {
			return errors.New("dingtalk.client_id and dingtalk.client_secret must be configured (replace placeholder values) when bot.platform = \"dingtalk\"")
		}
	}
	cfg.Bot.Platform = platform
	return nil
}

func credentialConfigured(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value != "" && value != "xxx" && !strings.Contains(value, "xxxxxxxx")
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
