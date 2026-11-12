package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPathDeduplicates(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/custom/bin")
	got := buildPath("/home/u/.local/bin/nectar", "/usr/bin", "/bin")
	want := "/home/u/.local/bin:/usr/bin:/bin:/custom/bin"
	if got != want {
		t.Fatalf("buildPath() = %q, want %q", got, want)
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := ExpandPath("~/.nectar/config.toml"); got != filepath.Join(home, ".nectar", "config.toml") {
		t.Fatalf("ExpandPath() = %q", got)
	}
}

func TestManagerValidate(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "nectar")
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Manager{BinaryPath: binaryPath, ConfigPath: configPath}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected missing config error")
	}
}

func TestXMLEscape(t *testing.T) {
	got := xmlEscape(`a&b<c"d`)
	if !strings.Contains(got, "&amp;") || !strings.Contains(got, "&lt;") || !strings.Contains(got, "&#34;") {
		t.Fatalf("xmlEscape() = %q", got)
	}
}
