package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ResolveModel reads OpenCode's own config when Nectar has no explicit model.
func ResolveModel(model string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}

	path := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG"))
	if path == "" {
		base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			base = filepath.Join(home, ".config")
		}
		path = filepath.Join(base, "opencode", "opencode.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Model)
}
