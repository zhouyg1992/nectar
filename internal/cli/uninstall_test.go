package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveManagedDataDirSafety(t *testing.T) {
	home := t.TempDir()
	if err := removeManagedDataDir(home, home); err == nil {
		t.Fatal("expected home directory purge to be rejected")
	}
	dataDir := filepath.Join(home, ".nectar")
	if err := os.MkdirAll(filepath.Join(dataDir, "workspace"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedDataDir(dataDir, home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir still exists: %v", err)
	}
}

func TestPathWithin(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nectar")
	if !pathWithin(filepath.Join(root, "config.toml"), root) {
		t.Fatal("child path should be within root")
	}
	if pathWithin(filepath.Join(root, "..", "outside"), root) {
		t.Fatal("sibling path should be outside root")
	}
}

func TestUninstallPathsFallBackToPartialConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.toml")
	dataDir := filepath.Join(home, "custom-data")
	workspace := filepath.Join(home, "repo")
	source := "[feishu]\napp_id = \"real-app\"\napp_secret = \"real-secret\"\n" +
		"[dingtalk]\nclient_id = \"real-ding\"\nclient_secret = \"real-secret\"\n" +
		"[codex]\nworking_dir = " + `"` + workspace + `"` + "\n" +
		"[storage]\ndata_dir = " + `"` + dataDir + `"` + "\n"
	if err := os.WriteFile(configPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	gotData, gotDirs, err := uninstallPaths(home, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotData != dataDir {
		t.Fatalf("data dir = %q, want %q", gotData, dataDir)
	}
	found := false
	for _, dir := range gotDirs {
		if dir == workspace {
			found = true
		}
	}
	if !found {
		t.Fatalf("workspace %q missing from %v", workspace, gotDirs)
	}
}
