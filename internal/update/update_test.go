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
	"strconv"
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

func TestDownloadResumesPartialFile(t *testing.T) {
	archive := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	asset := "nectar.tar.gz"
	offset := 10
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		http.ServeContent(w, r, asset, time.Time{}, bytes.NewReader(archive))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), asset)
	if err := os.WriteFile(destination+".part", archive[:offset], 0o644); err != nil {
		t.Fatal(err)
	}
	updater := New(Config{Attempts: 1})
	if err := updater.download(t.Context(), server.URL, destination, asset, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, archive) {
		t.Fatalf("downloaded %q, want %q", got, archive)
	}
	if len(ranges) != 1 || ranges[0] != fmt.Sprintf("bytes=%d-", offset) {
		t.Fatalf("range headers = %#v", ranges)
	}
	if _, err := os.Stat(destination + ".part"); !os.IsNotExist(err) {
		t.Fatalf("partial file still exists: %v", err)
	}
}

func TestDownloadRestartsWhenServerIgnoresRange(t *testing.T) {
	archive := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	asset := "nectar.tar.gz"
	offset := 10
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), asset)
	if err := os.WriteFile(destination+".part", archive[:offset], 0o644); err != nil {
		t.Fatal(err)
	}
	updater := New(Config{Attempts: 1})
	if err := updater.download(t.Context(), server.URL, destination, asset, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, archive) {
		t.Fatalf("downloaded %q, want %q", got, archive)
	}
	if len(ranges) != 1 || ranges[0] != fmt.Sprintf("bytes=%d-", offset) {
		t.Fatalf("range headers = %#v", ranges)
	}
}

func TestDownloadResumesAfterShortResponse(t *testing.T) {
	archive := bytes.Repeat([]byte("nectar"), 64)
	asset := "nectar.tar.gz"
	offset := 100
	var ranges []string
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		ranges = append(ranges, r.Header.Get("Range"))
		if requests == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
			_, _ = w.Write(archive[:offset])
			return
		}
		http.ServeContent(w, r, asset, time.Time{}, bytes.NewReader(archive))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), asset)
	updater := New(Config{Attempts: 2, RetryDelay: time.Millisecond})
	if err := updater.download(t.Context(), server.URL, destination, asset, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, archive) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(archive))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(ranges) != 2 || ranges[0] != "" || ranges[1] != fmt.Sprintf("bytes=%d-", offset) {
		t.Fatalf("range headers = %#v", ranges)
	}
}

func TestMirrorURLs(t *testing.T) {
	updater := New(Config{Repo: "owner/repo", Mirror: "https://mirror.example/"})
	if got, want := updater.downloadBase("v1.2.3"),
		"https://mirror.example/https://github.com/owner/repo/releases/download/v1.2.3"; got != want {
		t.Fatalf("download base = %q, want %q", got, want)
	}
	if got, want := updater.mirrorURL("https://api.github.com/repos/owner/repo/releases/latest"),
		"https://mirror.example/https://api.github.com/repos/owner/repo/releases/latest"; got != want {
		t.Fatalf("mirrored URL = %q, want %q", got, want)
	}
}

func TestDownloadBaseOverridesMirror(t *testing.T) {
	updater := New(Config{
		Repo:         "owner/repo",
		Mirror:       "https://mirror.example",
		DownloadBase: "https://base.example/releases/download/v1.2.3/",
	})
	if got, want := updater.downloadBase("v1.2.3"), "https://base.example/releases/download/v1.2.3"; got != want {
		t.Fatalf("download base = %q, want %q", got, want)
	}
}

func TestParseContentRange(t *testing.T) {
	cases := []struct {
		value string
		start int64
		end   int64
		total int64
		ok    bool
	}{
		{value: "bytes 10-19/100", start: 10, end: 19, total: 100, ok: true},
		{value: "bytes 0-0/1", start: 0, end: 0, total: 1, ok: true},
		{value: "bytes 10-19/*", start: 10, end: 19, total: -1, ok: true},
		{value: "bytes */100"},
		{value: "items 0-1/2"},
	}
	for _, tc := range cases {
		start, end, total, ok := parseContentRange(tc.value)
		if start != tc.start || end != tc.end || total != tc.total || ok != tc.ok {
			t.Fatalf("parseContentRange(%q) = %d, %d, %d, %v", tc.value, start, end, total, ok)
		}
	}
}

func TestRunCheckLatestThroughMirror(t *testing.T) {
	var pagePath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			pagePath = r.URL.Path
			http.Redirect(w, r, "/releases/tag/v0.4.0", http.StatusFound)
		case strings.HasSuffix(r.URL.Path, "/releases/tag/v0.4.0"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	updater := New(Config{
		Repo:           "owner/repo",
		CurrentVersion: "v0.3.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		Mirror:         server.URL,
		Attempts:       1,
	})
	result, err := updater.Run(t.Context(), Options{CheckOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetVersion != "v0.4.0" || !result.UpdateAvailable {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(pagePath, "https://github.com/owner/repo/releases/latest") {
		t.Fatalf("page path = %q", pagePath)
	}
}

func TestRunFallsBackToMirroredGitHubAPI(t *testing.T) {
	var apiPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "api.github.com"):
			apiPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"tag_name":"v0.4.0"}`)
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			http.Error(w, "edge unavailable", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	updater := New(Config{
		Repo:           "owner/repo",
		CurrentVersion: "v0.3.0",
		GOOS:           testGOOS,
		GOARCH:         testGOARCH,
		Mirror:         server.URL,
		Attempts:       1,
	})
	result, err := updater.Run(t.Context(), Options{CheckOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetVersion != "v0.4.0" || !result.UpdateAvailable {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(apiPath, "https://api.github.com/repos/owner/repo/releases/latest") {
		t.Fatalf("api path = %q", apiPath)
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
