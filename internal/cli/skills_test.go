package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshAgentSkillsRunsNewBinary(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "args.txt")
	binary := filepath.Join(dir, "nectar")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + marker + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	if err := refreshAgentSkills(binary, configPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	if !strings.Contains(got, "skills install --quiet --config "+configPath) {
		t.Fatalf("args = %q", got)
	}
}
