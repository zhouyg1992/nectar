package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var unsafeBotIDChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// ResolvedBot is a fully expanded bot configuration. Its Config can be passed
// to the existing single-bot runtime builders without special cases.
type ResolvedBot struct {
	ID          string
	Name        string
	Config      Config
	WorktreeDir string
}

// ResolveBots expands the legacy [bot] section and the newer [bots.<id>]
// entries into independent per-bot configs. Legacy configs keep the existing
// data directory. Workspace locking is per chat for every mode; shared mode
// controls directory sharing, not serialization.
func (c Config) ResolveBots() ([]ResolvedBot, error) {
	keys := make([]string, 0, len(c.Bots))
	for key := range c.Bots {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var bots []ResolvedBot
	if len(keys) == 0 || (c.hasLegacyCredentials() && !containsKey(keys, "default")) {
		if c.hasLegacyCredentials() {
			bot, err := c.resolveLegacyBot()
			if err != nil {
				return nil, err
			}
			bots = append(bots, bot)
		} else if len(keys) == 0 {
			return nil, fmt.Errorf("no bot credentials configured; add [bot] credentials or at least one [bots.<name>] entry")
		}
	}

	for _, key := range keys {
		bot, err := c.resolveBotEntry(key, c.Bots[key])
		if err != nil {
			return nil, err
		}
		bots = append(bots, bot)
	}
	if len(bots) == 0 {
		return nil, fmt.Errorf("no usable bots configured")
	}
	seenIDs := make(map[string]string, len(bots))
	seenCredentials := make(map[string]string, len(bots))
	for _, bot := range bots {
		if previous, ok := seenIDs[bot.ID]; ok {
			return nil, fmt.Errorf("duplicate bot id %q (used by %q and %q)", bot.ID, previous, bot.Name)
		}
		seenIDs[bot.ID] = bot.Name
		credentialKey := botCredentialKey(bot.Config)
		if previous, ok := seenCredentials[credentialKey]; ok {
			return nil, fmt.Errorf("bots %q and %q use the same %s credentials; each bot needs its own platform identity", previous, bot.ID, bot.Config.Bot.Platform)
		}
		seenCredentials[credentialKey] = bot.ID
	}

	sort.Slice(bots, func(i, j int) bool { return bots[i].ID < bots[j].ID })
	return bots, nil
}

func (c Config) resolveLegacyBot() (ResolvedBot, error) {
	cfg := c
	cfg.Bots = nil
	if strings.TrimSpace(cfg.Bot.WorkspaceMode) == "" {
		cfg.Bot.WorkspaceMode = "shared"
	}
	if err := validateWorkspaceMode("bot.workspace_mode", cfg.Bot.WorkspaceMode); err != nil {
		return ResolvedBot{}, err
	}
	if err := validateBotPlatform(&cfg, "default"); err != nil {
		return ResolvedBot{}, err
	}
	name := strings.TrimSpace(cfg.Bot.Name)
	if name == "" {
		name = "Nectar"
	}
	cfg.Bot.Name = name
	return ResolvedBot{ID: "default", Name: name, Config: cfg, WorktreeDir: c.Storage.DataDir}, nil
}

func (c Config) resolveBotEntry(key string, entry BotEntry) (ResolvedBot, error) {
	id := sanitizeBotID(key)
	if id == "" {
		return ResolvedBot{}, fmt.Errorf("bot %q has an empty id after sanitizing", key)
	}
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = key
	}

	cfg := c
	cfg.Bots = nil
	cfg.Feishu = copyFeishu(c.Feishu, entry.Feishu)
	cfg.DingTalk = copyDingTalk(c.DingTalk, entry.DingTalk)
	cfg.Telegram = copyTelegram(c.Telegram, entry.Telegram)
	normalizeResolvedPlatforms(&cfg)

	platform, err := resolveBotPlatform(entry.Platform, cfg.Feishu, cfg.DingTalk, cfg.Telegram)
	if err != nil {
		return ResolvedBot{}, fmt.Errorf("bot %q: %w", id, err)
	}
	engine := strings.TrimSpace(entry.Engine)
	if engine == "" {
		engine = strings.TrimSpace(c.Bot.Engine)
	}
	if engine == "" {
		engine = "codex"
	}
	switch engine {
	case "codex", "opencode", "cursor", "claude", "pi":
	default:
		return ResolvedBot{}, fmt.Errorf("bot %q has unsupported engine %q", id, engine)
	}

	dataDir := strings.TrimSpace(entry.DataDir)
	if dataDir == "" {
		dataDir = filepath.Join(c.Storage.DataDir, "bots", id)
	} else {
		expanded, err := expandHome(dataDir)
		if err != nil {
			return ResolvedBot{}, fmt.Errorf("bot %q data_dir: %w", id, err)
		}
		dataDir = expanded
	}
	cfg.Storage.DataDir = dataDir

	workspaceMode := strings.TrimSpace(entry.WorkspaceMode)
	if workspaceMode == "" {
		workspaceMode = "auto"
	}
	if err := validateWorkspaceMode("bots."+id+".workspace_mode", workspaceMode); err != nil {
		return ResolvedBot{}, err
	}
	cfg.Bot = BotConfig{
		Name:          name,
		Engine:        engine,
		Platform:      platform,
		AdminUsers:    cloneStrings(entry.AdminUsers),
		AllowedChats:  cloneStrings(entry.AllowedChats),
		WorkspaceMode: workspaceMode,
	}

	if workspace := strings.TrimSpace(entry.Workspace); workspace != "" {
		expanded, err := expandHome(workspace)
		if err != nil {
			return ResolvedBot{}, fmt.Errorf("bot %q workspace: %w", id, err)
		}
		applyEngineWorkspace(&cfg, expanded)
	}
	if model := strings.TrimSpace(entry.Model); model != "" {
		applyEngineModel(&cfg, engine, model)
	}
	if err := validateBotPlatform(&cfg, id); err != nil {
		return ResolvedBot{}, err
	}
	return ResolvedBot{ID: id, Name: name, Config: cfg, WorktreeDir: c.Storage.DataDir}, nil
}

func (c Config) hasLegacyCredentials() bool {
	return credentialConfigured(c.Feishu.AppID) && credentialConfigured(c.Feishu.AppSecret) ||
		credentialConfigured(c.DingTalk.ClientID) && credentialConfigured(c.DingTalk.ClientSecret) ||
		credentialConfigured(c.Telegram.BotToken)
}

func resolveBotPlatform(platform string, feishu FeishuConfig, dingtalk DingTalkConfig, telegram TelegramConfig) (string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform != "" {
		switch platform {
		case "feishu", "dingtalk", "telegram":
			return platform, nil
		default:
			return "", fmt.Errorf("unsupported platform %q (supported: feishu, dingtalk, telegram)", platform)
		}
	}
	var configured []string
	if credentialConfigured(feishu.AppID) && credentialConfigured(feishu.AppSecret) {
		configured = append(configured, "feishu")
	}
	if credentialConfigured(dingtalk.ClientID) && credentialConfigured(dingtalk.ClientSecret) {
		configured = append(configured, "dingtalk")
	}
	if credentialConfigured(telegram.BotToken) {
		configured = append(configured, "telegram")
	}
	switch len(configured) {
	case 0:
		return "", fmt.Errorf("no platform credentials configured")
	case 1:
		return configured[0], nil
	default:
		return "", fmt.Errorf("multiple platform credentials configured (%s); set platform explicitly", strings.Join(configured, ", "))
	}
}

func validateBotPlatform(cfg *Config, botID string) error {
	switch cfg.Bot.Platform {
	case "feishu":
		if !credentialConfigured(cfg.Feishu.AppID) || !credentialConfigured(cfg.Feishu.AppSecret) {
			return fmt.Errorf("bot %q requires feishu.app_id and feishu.app_secret", botID)
		}
	case "dingtalk":
		if !credentialConfigured(cfg.DingTalk.ClientID) || !credentialConfigured(cfg.DingTalk.ClientSecret) {
			return fmt.Errorf("bot %q requires dingtalk.client_id and dingtalk.client_secret", botID)
		}
	case "telegram":
		if !credentialConfigured(cfg.Telegram.BotToken) {
			return fmt.Errorf("bot %q requires telegram.bot_token", botID)
		}
	default:
		return fmt.Errorf("bot %q has unsupported platform %q", botID, cfg.Bot.Platform)
	}
	return nil
}

func normalizeResolvedPlatforms(cfg *Config) {
	cfg.Feishu.AppID = strings.TrimSpace(cfg.Feishu.AppID)
	cfg.Feishu.AppSecret = strings.TrimSpace(cfg.Feishu.AppSecret)
	cfg.Feishu.LogLevel = strings.ToLower(strings.TrimSpace(cfg.Feishu.LogLevel))
	if cfg.Feishu.LogLevel == "" {
		cfg.Feishu.LogLevel = "info"
	}

	cfg.DingTalk.ClientID = strings.TrimSpace(cfg.DingTalk.ClientID)
	cfg.DingTalk.ClientSecret = strings.TrimSpace(cfg.DingTalk.ClientSecret)
	cfg.DingTalk.AppKey = strings.TrimSpace(cfg.DingTalk.AppKey)
	cfg.DingTalk.AppSecret = strings.TrimSpace(cfg.DingTalk.AppSecret)
	cfg.DingTalk.RobotCode = strings.TrimSpace(cfg.DingTalk.RobotCode)
	cfg.DingTalk.LogLevel = strings.ToLower(strings.TrimSpace(cfg.DingTalk.LogLevel))
	if cfg.DingTalk.ClientID == "" {
		cfg.DingTalk.ClientID = cfg.DingTalk.AppKey
	}
	if cfg.DingTalk.AppKey == "" {
		cfg.DingTalk.AppKey = cfg.DingTalk.ClientID
	}
	if cfg.DingTalk.ClientSecret == "" {
		cfg.DingTalk.ClientSecret = cfg.DingTalk.AppSecret
	}
	if cfg.DingTalk.AppSecret == "" {
		cfg.DingTalk.AppSecret = cfg.DingTalk.ClientSecret
	}
	if cfg.DingTalk.RobotCode == "" {
		cfg.DingTalk.RobotCode = cfg.DingTalk.ClientID
	}
	if cfg.DingTalk.LogLevel == "" {
		cfg.DingTalk.LogLevel = "info"
	}

	cfg.Telegram.BotToken = strings.TrimSpace(cfg.Telegram.BotToken)
	cfg.Telegram.Proxy = strings.TrimSpace(cfg.Telegram.Proxy)
	cfg.Telegram.APIBaseURL = strings.TrimRight(strings.TrimSpace(cfg.Telegram.APIBaseURL), "/")
	cfg.Telegram.LogLevel = strings.ToLower(strings.TrimSpace(cfg.Telegram.LogLevel))
	if cfg.Telegram.LogLevel == "" {
		cfg.Telegram.LogLevel = "info"
	}
}

func botCredentialKey(cfg Config) string {
	switch cfg.Bot.Platform {
	case "feishu":
		return "feishu:" + strings.ToLower(strings.TrimSpace(cfg.Feishu.AppID))
	case "dingtalk":
		return "dingtalk:" + strings.ToLower(strings.TrimSpace(cfg.DingTalk.ClientID))
	case "telegram":
		return "telegram:" + strings.TrimSpace(cfg.Telegram.BotToken)
	default:
		return "unknown:" + cfg.Bot.Platform
	}
}

func validateWorkspaceMode(name, value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "shared", "per_chat", "per_session", "worktree", "auto":
		return nil
	default:
		return fmt.Errorf("unsupported %s %q (supported: shared, per_chat, per_session, worktree, auto)", name, value)
	}
}

func sanitizeBotID(value string) string {
	value = unsafeBotIDChars.ReplaceAllString(strings.TrimSpace(value), "-")
	value = strings.Trim(value, ".-")
	if len(value) > 64 {
		value = value[:64]
	}
	return value
}

func applyEngineWorkspace(cfg *Config, workspace string) {
	cfg.Codex.WorkingDir = workspace
	cfg.OpenCode.WorkingDir = workspace
	cfg.Cursor.WorkingDir = workspace
	cfg.Claude.WorkingDir = workspace
	cfg.Pi.WorkingDir = workspace
}

func applyEngineModel(cfg *Config, engineName, model string) {
	switch engineName {
	case "codex":
		cfg.Codex.Model = model
	case "opencode":
		cfg.OpenCode.Model = model
	case "cursor":
		cfg.Cursor.Model = model
	case "claude":
		cfg.Claude.Model = model
	case "pi":
		cfg.Pi.Model = model
	}
}

func copyFeishu(base FeishuConfig, override *FeishuConfig) FeishuConfig {
	if override != nil {
		return *override
	}
	return base
}

func copyDingTalk(base DingTalkConfig, override *DingTalkConfig) DingTalkConfig {
	if override != nil {
		return *override
	}
	return base
}

func copyTelegram(base TelegramConfig, override *TelegramConfig) TelegramConfig {
	if override != nil {
		return *override
	}
	return base
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func containsKey(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
