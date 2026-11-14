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
		for _, name := range []string{ScheduleSkillName, MemorySkillName} {
			data, err := os.ReadFile(filepath.Join(root, name, "SKILL.md"))
			if err != nil {
				t.Fatalf("%s/%s: %v", root, name, err)
			}
			if !strings.Contains(string(data), "name: "+name) {
				t.Fatalf("%s/%s does not contain the skill", root, name)
			}
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
	if strings.Count(string(data), memoryBlockBegin) != 1 {
		t.Fatalf("memory AGENTS block count = %d", strings.Count(string(data), memoryBlockBegin))
	}
	if !strings.Contains(string(data), "${NECTAR_CLI:-nectar}") {
		t.Fatal("AGENTS hint does not use NECTAR_CLI")
	}
	if !strings.Contains(string(data), "memory add") {
		t.Fatal("AGENTS hint does not describe memory")
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
	if strings.Count(string(data), memoryBlockBegin) != 1 {
		t.Fatalf("memory AGENTS block duplicated: %d", strings.Count(string(data), memoryBlockBegin))
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

func TestUninstallSkillAndWorkspaceHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	dataDir := filepath.Join(home, ".nectar")
	configPath := filepath.Join(dataDir, "config.toml")
	workspace := filepath.Join(dataDir, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(workspace, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# User rules\n\n- keep me\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Home:       home,
		DataDir:    dataDir,
		ConfigPath: configPath,
	}
	if err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(UninstallOptions{
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
		for _, name := range []string{ScheduleSkillName, MemorySkillName} {
			if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
				t.Fatalf("skill still exists under %s/%s: %v", root, name, err)
			}
		}
	}
	data, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- keep me") {
		t.Fatal("user AGENTS.md content was removed")
	}
	if strings.Contains(string(data), blockBegin) || strings.Contains(string(data), blockEnd) {
		t.Fatal("managed schedule block was not removed")
	}
	if strings.Contains(string(data), memoryBlockBegin) || strings.Contains(string(data), memoryBlockEnd) {
		t.Fatal("managed memory block was not removed")
	}
	info, err := os.Stat(agents)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("AGENTS.md mode = %o, want 640", info.Mode().Perm())
	}
	if err := Uninstall(UninstallOptions{
		Home:       home,
		DataDir:    dataDir,
		ConfigPath: configPath,
	}); err != nil {
		t.Fatalf("second uninstall should be idempotent: %v", err)
	}
}

func TestUninstallDryRunKeepsSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	dataDir := filepath.Join(home, ".nectar")
	configPath := filepath.Join(dataDir, "config.toml")
	if err := Install(Options{
		Home:        home,
		DataDir:     dataDir,
		ConfigPath:  configPath,
		NoWorkspace: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(UninstallOptions{
		Home:        home,
		DataDir:     dataDir,
		ConfigPath:  configPath,
		NoWorkspace: true,
		DryRun:      true,
		Quiet:       true,
	}); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(dataDir, "skills", ScheduleSkillName, "SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("dry run removed skill: %v", err)
	}
	memorySkill := filepath.Join(dataDir, "skills", MemorySkillName, "SKILL.md")
	if _, err := os.Stat(memorySkill); err != nil {
		t.Fatalf("dry run removed memory skill: %v", err)
	}
}
