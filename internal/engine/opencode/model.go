package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type fileConfig struct {
	Model    string `json:"model"`
	Provider map[string]struct {
		Models map[string]struct {
			Limit struct {
				Context int `json:"context"`
			} `json:"limit"`
		} `json:"models"`
	} `json:"provider"`
}

// ResolveModel reads OpenCode's own config when Nectar has no explicit model.
func ResolveModel(model string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}

	cfg, ok := loadFileConfig()
	if !ok {
		return ""
	}
	return strings.TrimSpace(cfg.Model)
}

// ResolveContextWindow returns the override or the context limit declared for
// the model in OpenCode's config. OpenCode does not include this value in
// `run --format json` events.
func ResolveContextWindow(model string, override int) int {
	if override > 0 {
		return override
	}

	cfg, ok := loadFileConfig()
	if !ok {
		return 0
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(cfg.Model)
	}
	if model == "" {
		return 0
	}
	model = strings.TrimSpace(strings.SplitN(model, "#", 2)[0])
	if model == "" {
		return 0
	}

	if providerID, modelID, found := strings.Cut(model, "/"); found {
		if provider, ok := cfg.Provider[providerID]; ok {
			if entry, ok := provider.Models[modelID]; ok && entry.Limit.Context > 0 {
				return entry.Limit.Context
			}
		}
	}

	// Be tolerant of configs that use a model ID without its provider prefix.
	for _, provider := range cfg.Provider {
		if entry, ok := provider.Models[model]; ok && entry.Limit.Context > 0 {
			return entry.Limit.Context
		}
	}
	return 0
}

func loadFileConfig() (fileConfig, bool) {
	path := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG"))
	if path == "" {
		base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fileConfig{}, false
			}
			base = filepath.Join(home, ".config")
		}
		path = filepath.Join(base, "opencode", "opencode.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, false
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, false
	}
	return cfg, true
}
