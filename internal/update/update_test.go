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
	"time"
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

	var progress []Progress
	updater := New(Config{
		Repo:           "owner/repo",
		CurrentVersion: "v0.1.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		DownloadBase:   server.URL,
		ExecutablePath: targetPath,
		Progress: func(value Progress) {
			progress = append(progress, value)
		},
	})
	result, err := updater.Run(t.Context(), Options{Version: "v0.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || !result.UpdateAvailable || result.TargetVersion != "v0.2.0" {
		t.Fatalf("result = %#v", result)
	}
	if result.ExecutablePath != targetPath {
		t.Fatalf("installed path = %q, want %q", result.ExecutablePath, targetPath)
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
	if len(progress) == 0 {
		t.Fatal("no download progress reported")
	}
	final := progress[len(progress)-1]
	if !final.Done || final.Downloaded != int64(len(archive)) || final.Total != int64(len(archive)) {
		t.Fatalf("final progress = %#v, archive bytes = %d", final, len(archive))
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

func TestRunFallsBackToGitHubAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "edge unavailable", http.StatusBadGateway)
	})
	mux.HandleFunc("/api/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tag_name":"v0.3.0"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	updater := New(Config{
		CurrentVersion: "v0.2.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		LatestURL:      server.URL + "/latest",
		LatestAPIURL:   server.URL + "/api/latest",
		Attempts:       1,
	})
	result, err := updater.Run(t.Context(), Options{CheckOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetVersion != "v0.3.0" || !result.UpdateAvailable {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunRetriesTransientDownload(t *testing.T) {
	targetPath := filepath.Join(t.TempDir(), "nectar")
	if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	asset := assetName("v0.2.0", testGOOS, testGOARCH)
	archive := makeArchive(t, "new binary")
	archiveRequests := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		archiveRequests++
		if archiveRequests == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  %s\n", checksum(archive), asset)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	updater := New(Config{
		CurrentVersion: "v0.1.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		DownloadBase:   server.URL,
		ExecutablePath: targetPath,
		Attempts:       2,
		RetryDelay:     time.Millisecond,
	})
	result, err := updater.Run(t.Context(), Options{Version: "v0.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Fatalf("result = %#v", result)
	}
	if archiveRequests != 2 {
		t.Fatalf("archive requests = %d, want 2", archiveRequests)
	}
}

func TestDefaultClientHasNoOverallTimeout(t *testing.T) {
	updater := New(Config{})
	if updater.client.Timeout != 0 {
		t.Fatalf("client timeout = %s, want no overall timeout", updater.client.Timeout)
	}
	transport, ok := updater.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T", updater.client.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Fatal("response header timeout is not configured")
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
