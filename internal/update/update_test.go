package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testGOOS   = "darwin"
	testGOARCH = "arm64"
)

func TestRunInstallsRelease(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "nectar")
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	asset := assetName("v0.2.0", testGOOS, testGOARCH)
	archive := makeArchive(t, "new binary")
	server := newReleaseServer(t, asset, archive, checksum(archive))
	defer server.Close()

	updater := New(Config{
		Repo:           "owner/repo",
		CurrentVersion: "v0.1.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		DownloadBase:   server.URL,
		ExecutablePath: targetPath,
	})
	result, err := updater.Run(t.Context(), Options{Version: "v0.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || !result.UpdateAvailable || result.TargetVersion != "v0.2.0" {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new binary" {
		t.Fatalf("target content = %q", data)
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("target mode = %o", info.Mode().Perm())
	}
}

func TestRunRefreshesSkillsAfterInstall(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "nectar")
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	asset := assetName("v0.2.0", testGOOS, testGOARCH)
	archive := makeArchive(t, "new binary")
	server := newReleaseServer(t, asset, archive, checksum(archive))
	defer server.Close()

	refreshed := ""
	updater := New(Config{
		Repo:           "owner/repo",
		CurrentVersion: "v0.1.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		DownloadBase:   server.URL,
		ExecutablePath: targetPath,
		RefreshSkills: func(binaryPath string) error {
			refreshed = binaryPath
			return nil
		},
	})
	result, err := updater.Run(t.Context(), Options{Version: "v0.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed != want {
		t.Fatalf("refreshed %q, want %q", refreshed, want)
	}
	if !result.SkillsUpdated {
		t.Fatal("SkillsUpdated = false")
	}
}

func TestRunCheckLatest(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "nectar")
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v0.2.0", http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/v0.2.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	updater := New(Config{
		Repo:           "owner/repo",
		CurrentVersion: "v0.1.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		LatestURL:      server.URL + "/latest",
		ExecutablePath: targetPath,
	})
	result, err := updater.Run(t.Context(), Options{CheckOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable || result.Updated || result.TargetVersion != "v0.2.0" {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("check changed binary to %q", data)
	}
}

func TestRunRejectsChecksumMismatch(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "nectar")
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	asset := assetName("v0.2.0", testGOOS, testGOARCH)
	archive := makeArchive(t, "new binary")
	server := newReleaseServer(t, asset, archive, strings.Repeat("0", 64))
	defer server.Close()

	updater := New(Config{
		Repo:           "owner/repo",
		CurrentVersion: "v0.1.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		DownloadBase:   server.URL,
		ExecutablePath: targetPath,
	})
	_, err := updater.Run(t.Context(), Options{Version: "v0.2.0"})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v", err)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("failed update changed binary to %q", data)
	}
}

func TestRunSameVersionDoesNotDownload(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "nectar")
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	updater := New(Config{
		Repo:           "owner/repo",
		CurrentVersion: "v0.2.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		DownloadBase:   "http://127.0.0.1:1",
		ExecutablePath: targetPath,
	})
	result, err := updater.Run(t.Context(), Options{Version: "v0.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if result.UpdateAvailable || result.Updated {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunDevCheckOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v9.0.0", http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/v9.0.0", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	updater := New(Config{
		CurrentVersion: "dev",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		LatestURL:      server.URL + "/latest",
	})
	result, err := updater.Run(t.Context(), Options{CheckOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable || result.TargetVersion != "v9.0.0" {
		t.Fatalf("result = %#v", result)
	}
}

func newReleaseServer(t *testing.T, asset string, archive []byte, checksumValue string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", checksumValue, asset)
	})
	return httptest.NewServer(mux)
}

func makeArchive(t *testing.T, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	data := []byte(content)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: "nectar",
		Mode: 0o755,
		Size: int64(len(data)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
