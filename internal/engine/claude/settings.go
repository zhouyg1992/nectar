package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type settingsFile struct {
	Model string `json:"model"`
}

// ResolveModel returns the model Nectar should display. An explicit Nectar
// model wins, then ANTHROPIC_MODEL, then Claude Code's own settings.
func ResolveModel(model string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}
	if env := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")); env != "" {
		return env
	}
	home := claudeHome()
	if home == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		return ""
	}
	var settings settingsFile
	if err := json.Unmarshal(data, &settings); err != nil {
		return ""
	}
	return strings.TrimSpace(settings.Model)
}

func claudeHome() string {
	if home := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".claude")
}
