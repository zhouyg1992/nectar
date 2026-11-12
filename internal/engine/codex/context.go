package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type codexConfig struct {
	Model            string `toml:"model"`
	ModelCatalogJSON string `toml:"model_catalog_json"`
}

type modelCatalog struct {
	Models []modelEntry `json:"models"`
}

type modelEntry struct {
	Slug          string `json:"slug"`
	ContextWindow int    `json:"context_window"`
}

// ResolveContextWindow reads Codex's own config and model catalog. The
// override always wins; zero means unknown and the card omits the percentage.
func ResolveContextWindow(model string, override int) int {
	if override > 0 {
		return override
	}

	home := codexHome()
	if home == "" {
		return 0
	}

	var cfg codexConfig
	if _, err := toml.DecodeFile(filepath.Join(home, "config.toml"), &cfg); err != nil {
		return 0
	}
	if strings.TrimSpace(model) == "" {
		model = cfg.Model
	}
	model = strings.TrimSpace(model)
	catalogPath := strings.TrimSpace(cfg.ModelCatalogJSON)
	if model == "" || catalogPath == "" {
		return 0
	}
	catalogPath = expandHome(catalogPath)
	if !filepath.IsAbs(catalogPath) {
		catalogPath = filepath.Join(home, catalogPath)
	}

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return 0
	}
	var catalog modelCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return 0
	}
	for _, entry := range catalog.Models {
		if entry.Slug == model || strings.HasSuffix(entry.Slug, "/"+model) {
			return entry.ContextWindow
		}
	}
	return 0
}

// ResolveModel returns the model Nectar should display. An explicit Nectar
// model wins; otherwise use the model configured in Codex itself.
func ResolveModel(model string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}
	home := codexHome()
	if home == "" {
		return ""
	}
	var cfg codexConfig
	if _, err := toml.DecodeFile(filepath.Join(home, "config.toml"), &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Model)
}

func codexHome() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".codex")
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
