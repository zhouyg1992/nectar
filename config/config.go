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
	if v := os.Getenv("NECTAR_ENGINE"); v != "" {
		cfg.Bot.Engine = v
	}
	if v := os.Getenv("NECTAR_DATA_DIR"); v != "" {
		cfg.Storage.DataDir = v
	}
	if v := os.Getenv("NECTAR_WORKING_DIR"); v != "" {
		cfg.Codex.WorkingDir = v
		cfg.OpenCode.WorkingDir = v
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
	if cfg.Storage.DataDir == "" {
		return errors.New("storage.data_dir is required")
	}
	if cfg.Codex.WorkingDir == "" {
		return errors.New("codex.working_dir is required")
	}
	if cfg.OpenCode.WorkingDir == "" {
		return errors.New("opencode.working_dir is required")
	}
	if strings.TrimSpace(cfg.Codex.Path) == "" {
		cfg.Codex.Path = "codex"
	}
	if strings.TrimSpace(cfg.OpenCode.Path) == "" {
		cfg.OpenCode.Path = "opencode"
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
	case "codex", "opencode":
	default:
		return fmt.Errorf("unsupported bot.engine %q (supported: codex, opencode)", engine)
	}
	cfg.Bot.Engine = engine
	cfg.Bot.AgentID = engine
	if cfg.Codex.MaxOutputBytes <= 0 {
		return errors.New("codex.max_output_bytes must be positive")
	}
	if cfg.OpenCode.MaxOutputBytes <= 0 {
		return errors.New("opencode.max_output_bytes must be positive")
	}
	if _, err := cfg.Timeout(); err != nil {
		return err
	}
	if _, err := cfg.OpenCodeTimeout(); err != nil {
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
