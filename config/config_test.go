package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsAndEnv(t *testing.T) {
	t.Setenv("FEISHU_APP_ID", "cli_env")
	t.Setenv("FEISHU_APP_SECRET", "secret_env")
	t.Setenv("NECTAR_DATA_DIR", t.TempDir())

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Feishu.AppID != "cli_env" || cfg.Feishu.AppSecret != "secret_env" {
		t.Fatalf("env override failed: %#v", cfg.Feishu)
	}
	if cfg.Bot.Engine != "codex" || cfg.Bot.Name != "Nectar" {
		t.Fatalf("bot defaults = %#v", cfg.Bot)
	}
	timeout, err := cfg.Timeout()
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 30*time.Minute {
		t.Fatalf("timeout = %v", timeout)
	}
}

func TestLoadRejectsInvalidTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[codex]\ntimeout = \"soon\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}

func TestScheduleDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ScheduleEnabled() {
		t.Fatal("schedule should be enabled by default")
	}
	if cfg.Schedule.Timezone != "Local" {
		t.Fatalf("timezone = %q", cfg.Schedule.Timezone)
	}
	grace, err := cfg.ScheduleGrace()
	if err != nil {
		t.Fatal(err)
	}
	if grace != 5*time.Minute {
		t.Fatalf("grace = %s", grace)
	}
	if cfg.Schedule.HistoryLimit != 100 {
		t.Fatalf("history limit = %d", cfg.Schedule.HistoryLimit)
	}
	if cfg.Schedule.MaxConcurrent != 4 {
		t.Fatalf("max concurrent = %d", cfg.Schedule.MaxConcurrent)
	}
	jitter, err := cfg.ScheduleJitter()
	if err != nil {
		t.Fatal(err)
	}
	if jitter != 0 {
		t.Fatalf("jitter = %s", jitter)
	}
	timeout, err := cfg.ScheduleTimeout()
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 30*time.Minute {
		t.Fatalf("timeout = %s", timeout)
	}
}

func TestScheduleRejectsNegativeHistoryLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[schedule]\nhistory_limit = -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected negative history_limit error")
	}
}

func TestScheduleRejectsInvalidTimezone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[schedule]\ntimezone = \"Mars/Olympus\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid timezone error")
	}
}

func TestScheduleRejectsInvalidRuntimeLimits(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "negative max concurrent", body: "[schedule]\nmax_concurrent = -1\n"},
		{name: "negative jitter", body: "[schedule]\njitter = \"-1s\"\n"},
		{name: "invalid jitter", body: "[schedule]\njitter = \"soon\"\n"},
		{name: "negative timeout", body: "[schedule]\ntimeout = \"-1s\"\n"},
		{name: "invalid timeout", body: "[schedule]\ntimeout = \"soon\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestScheduleAllowsZeroRuntimeLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[schedule]\nmax_concurrent = 0\ntimeout = \"0s\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	timeout, err := cfg.ScheduleTimeout()
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 0 {
		t.Fatalf("timeout = %s, want 0", timeout)
	}
}

func TestDingTalkAutoDetect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	source := "[dingtalk]\nclient_id = \"dingreal\"\nclient_secret = \"secret\"\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Platform != "dingtalk" {
		t.Fatalf("platform = %q", cfg.Bot.Platform)
	}
	if cfg.DingTalk.RobotCode != "dingreal" || cfg.DingTalk.LogLevel != "info" {
		t.Fatalf("dingtalk = %#v", cfg.DingTalk)
	}
}

func TestDingTalkAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	source := "[bot]\nplatform = \"dingtalk\"\n\n[dingtalk]\napp_key = \"dingreal\"\napp_secret = \"secret\"\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DingTalk.ClientID != "dingreal" || cfg.DingTalk.ClientSecret != "secret" {
		t.Fatalf("dingtalk = %#v", cfg.DingTalk)
	}
}

func TestDingTalkCardDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	source := "[dingtalk]\nclient_id = \"dingreal\"\nclient_secret = \"secret\"\ncard_enabled = false\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DingTalk.CardEnabled == nil || *cfg.DingTalk.CardEnabled {
		t.Fatalf("card_enabled = %#v", cfg.DingTalk.CardEnabled)
	}
}

func TestTelegramAutoDetect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	source := "[telegram]\nbot_token = \"test-token\"\nproxy = \"http://127.0.0.1:7890\"\napi_base_url = \"https://api.example.test/\"\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Platform != "telegram" {
		t.Fatalf("platform = %q", cfg.Bot.Platform)
	}
	if cfg.Telegram.BotToken != "test-token" ||
		cfg.Telegram.Proxy != "http://127.0.0.1:7890" ||
		cfg.Telegram.APIBaseURL != "https://api.example.test" ||
		cfg.Telegram.LogLevel != "info" {
		t.Fatalf("telegram = %#v", cfg.Telegram)
	}
}

func TestTelegramRequiresCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\nplatform = \"telegram\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected telegram credential error")
	}
}

func TestTelegramEnvOverride(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "123456:env-token")
	t.Setenv("NECTAR_DATA_DIR", t.TempDir())
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Platform != "telegram" || cfg.Telegram.BotToken != "123456:env-token" {
		t.Fatalf("platform = %q telegram = %#v", cfg.Bot.Platform, cfg.Telegram)
	}
}

func TestBothPlatformsRequireSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	source := "[feishu]\napp_id = \"cli_real\"\napp_secret = \"secret1\"\n\n[dingtalk]\nclient_id = \"dingreal\"\nclient_secret = \"secret2\"\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected platform selection error")
	}
}

func TestDingTalkRequiresCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\nplatform = \"dingtalk\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected dingtalk credential error")
	}
}

func TestPlaceholderCredentialsDoNotSelectPlatform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	source := "[feishu]\napp_id = \"cli_xxxxxxxxxxxxxxxx\"\napp_secret = \"xxxxxxxx\"\n\n[dingtalk]\nclient_id = \"dingxxxxxxxx\"\nclient_secret = \"xxxxxxxx\"\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Platform != "feishu" {
		t.Fatalf("platform = %q", cfg.Bot.Platform)
	}
}

func TestEngineSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\nengine = \"opencode\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Engine != "opencode" {
		t.Fatalf("bot = %#v", cfg.Bot)
	}
	if cfg.OpenCode.Path != "opencode" || cfg.OpenCode.MaxOutputBytes <= 0 {
		t.Fatalf("opencode defaults = %#v", cfg.OpenCode)
	}
}

func TestOpenCodeContextWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[opencode]\ncontext_window = 200000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenCode.ContextWindow != 200000 {
		t.Fatalf("opencode context window = %d", cfg.OpenCode.ContextWindow)
	}
}

func TestPiDefaultsAndSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	source := "[bot]\nengine = \"pi\"\n\n[pi]\nprovider = \"anthropic\"\nmodel = \"claude-sonnet-4-5\"\nthinking = \"high\"\ncontext_window = 200000\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_PATH", "/opt/homebrew/bin/pi")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Engine != "pi" {
		t.Fatalf("bot = %#v", cfg.Bot)
	}
	if cfg.Pi.Path != "/opt/homebrew/bin/pi" || cfg.Pi.MaxOutputBytes <= 0 {
		t.Fatalf("pi defaults = %#v", cfg.Pi)
	}
	if !cfg.Pi.TrustWorkspace {
		t.Fatalf("pi trust workspace default = %#v", cfg.Pi)
	}
	if cfg.Pi.Provider != "anthropic" || cfg.Pi.Model != "claude-sonnet-4-5" || cfg.Pi.Thinking != "high" {
		t.Fatalf("pi config = %#v", cfg.Pi)
	}
	if cfg.Pi.ContextWindow != 200000 {
		t.Fatalf("pi context window = %d", cfg.Pi.ContextWindow)
	}
	timeout, err := cfg.PiTimeout()
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 30*time.Minute {
		t.Fatalf("pi timeout = %v", timeout)
	}
}

func TestLoadRejectsInvalidPiThinking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[pi]\nthinking = \"extreme\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid pi.thinking error")
	}
}

func TestInvalidEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\nengine = \"unknown\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unsupported engine error")
	}
}

func TestCursorEngineSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(
		"[bot]\nengine = \"cursor\"\n\n[cursor]\ncontext_window = 200000\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Engine != "cursor" {
		t.Fatalf("bot = %#v", cfg.Bot)
	}
	if cfg.Cursor.Path != "agent" || cfg.Cursor.MaxOutputBytes <= 0 {
		t.Fatalf("cursor defaults = %#v", cfg.Cursor)
	}
	if !cfg.Cursor.AutoApprove || !cfg.Cursor.TrustWorkspace {
		t.Fatalf("cursor non-interactive defaults = %#v", cfg.Cursor)
	}
	if cfg.Cursor.ContextWindow != 200000 {
		t.Fatalf("cursor context window = %d", cfg.Cursor.ContextWindow)
	}
	timeout, err := cfg.CursorTimeout()
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 30*time.Minute {
		t.Fatalf("timeout = %v", timeout)
	}
}

func TestCursorRejectsInvalidModeAndSandbox(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"mode":    "[cursor]\nmode = \"yolo\"\n",
		"sandbox": "[cursor]\nsandbox = \"maybe\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".toml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestClaudeEngineSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(
		"[bot]\nengine = \"claude\"\n\n[claude]\ncontext_window = 200000\npermission_mode = \"acceptEdits\"\nmax_turns = 12\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Engine != "claude" {
		t.Fatalf("bot = %#v", cfg.Bot)
	}
	if cfg.Claude.Path != "claude" || cfg.Claude.MaxOutputBytes <= 0 {
		t.Fatalf("claude defaults = %#v", cfg.Claude)
	}
	if !cfg.Claude.AutoApprove {
		t.Fatalf("claude auto approve default = %#v", cfg.Claude)
	}
	if cfg.Claude.ContextWindow != 200000 || cfg.Claude.PermissionMode != "acceptEdits" || cfg.Claude.MaxTurns != 12 {
		t.Fatalf("claude config = %#v", cfg.Claude)
	}
	timeout, err := cfg.ClaudeTimeout()
	if err != nil {
		t.Fatal(err)
	}
	if timeout != 30*time.Minute {
		t.Fatalf("timeout = %v", timeout)
	}
}

func TestClaudeRejectsInvalidPermissionMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[claude]\npermission_mode = \"yolo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error")
	}
}
