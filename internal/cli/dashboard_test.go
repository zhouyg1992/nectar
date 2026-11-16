package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardCLIRequiresEnabledConfig(t *testing.T) {
	configPath, _ := writeMemoryCLIConfig(t, "")
	if err := runDashboard([]string{"--config", configPath}); err == nil {
		t.Fatal("expected disabled dashboard error")
	}
}

func TestDashboardCLIPrintsLoginURLAndCreatesToken(t *testing.T) {
	configPath, dataDir := writeDashboardCLIConfig(t)

	out := captureMemoryCLIOutput(t, func() error {
		return runDashboard([]string{"--config", configPath, "--no-browser"})
	})
	if !strings.Contains(out, "/login?token=") || !strings.Contains(out, "127.0.0.1:8787") {
		t.Fatalf("dashboard output = %q", out)
	}
	tokenPath := filepath.Join(dataDir, "run", "dashboard.token")
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token permissions = %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimSpace(string(data))) < 16 {
		t.Fatalf("token too short: %q", string(data))
	}
}

func TestDashboardCLIOpensBrowserByDefault(t *testing.T) {
	configPath, _ := writeDashboardCLIConfig(t)
	previous := openDashboardBrowser
	var opened string
	openDashboardBrowser = func(rawURL string) error {
		opened = rawURL
		return nil
	}
	defer func() { openDashboardBrowser = previous }()

	captureMemoryCLIOutput(t, func() error {
		return runDashboard([]string{"--config", configPath})
	})
	if !strings.Contains(opened, "/login?token=") || !strings.Contains(opened, "127.0.0.1:8787") {
		t.Fatalf("opened URL = %q", opened)
	}
}

func TestDashboardCLINoBrowserFlag(t *testing.T) {
	configPath, _ := writeDashboardCLIConfig(t)
	previous := openDashboardBrowser
	openDashboardBrowser = func(rawURL string) error {
		t.Fatalf("unexpected browser open for %q", rawURL)
		return nil
	}
	defer func() { openDashboardBrowser = previous }()

	captureMemoryCLIOutput(t, func() error {
		return runDashboard([]string{"--config", configPath, "--no-browser"})
	})
}

func writeDashboardCLIConfig(t *testing.T) (string, string) {
	t.Helper()
	configPath, dataDir := writeMemoryCLIConfig(t, "")
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n[dashboard]\nenabled = true\nlisten = \"127.0.0.1:8787\"\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return configPath, dataDir
}
