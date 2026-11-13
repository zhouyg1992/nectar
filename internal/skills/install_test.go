package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSkillAndWorkspaceHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	dataDir := filepath.Join(home, ".nectar")
	configPath := filepath.Join(dataDir, "config.toml")

	if err := Install(Options{
		Home:       home,
		DataDir:    dataDir,
		ConfigPath: configPath,
	}); err != nil {
		t.Fatal(err)
	}
	roots := []string{
		filepath.Join(dataDir, "skills"),
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
	for _, root := range roots {
		data, err := os.ReadFile(filepath.Join(root, ScheduleSkillName, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		if !strings.Contains(string(data), "name: nectar-schedule") {
			t.Fatalf("%s does not contain the skill", root)
		}
	}

	agents := filepath.Join(home, ".nectar", "workspace", "AGENTS.md")
	data, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), blockBegin) != 1 {
		t.Fatalf("AGENTS block count = %d", strings.Count(string(data), blockBegin))
	}
	if !strings.Contains(string(data), "${NECTAR_CLI:-nectar}") {
		t.Fatal("AGENTS hint does not use NECTAR_CLI")
	}

	if err := os.WriteFile(agents, append([]byte("# User rules\n\n- keep me\n\n"), data...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(Options{Home: home, DataDir: dataDir, ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- keep me") {
		t.Fatal("user AGENTS.md content was lost")
	}
	if strings.Count(string(data), blockBegin) != 1 {
		t.Fatalf("AGENTS block duplicated: %d", strings.Count(string(data), blockBegin))
	}
}

func TestInstallNoWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	if err := Install(Options{
		Home:        home,
		DataDir:     filepath.Join(home, ".nectar"),
		ConfigPath:  filepath.Join(home, ".nectar", "config.toml"),
		NoWorkspace: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".nectar", "workspace", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected AGENTS.md: %v", err)
	}
}
