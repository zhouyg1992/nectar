package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type settingsFile struct {
	DefaultProvider string `json:"defaultProvider"`
	DefaultModel    string `json:"defaultModel"`
}

// ResolveModel returns the model Nectar should display. An explicit Nectar
// model wins, then Pi's own default model setting.
func ResolveModel(model string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}
	home := piHome()
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
	model = strings.TrimSpace(settings.DefaultModel)
	provider := strings.TrimSpace(settings.DefaultProvider)
	if model == "" || provider == "" || strings.Contains(model, "/") {
		return model
	}
	return provider + "/" + model
}

func piHome() string {
	if home := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".pi", "agent")
}
