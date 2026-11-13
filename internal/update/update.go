package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultRepo = "zhouyg1992/nectar"

type Config struct {
	Repo           string
	CurrentVersion string
	GOOS           string
	GOARCH         string
	HTTPClient     *http.Client
	DownloadBase   string
	LatestURL      string
	ExecutablePath string
	Logf           func(string, ...any)
}

type Options struct {
	Version   string
	CheckOnly bool
	Force     bool
}

type Result struct {
	CurrentVersion  string
	TargetVersion   string
	UpdateAvailable bool
	Updated         bool
}

type Updater struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) *Updater {
	if strings.TrimSpace(cfg.Repo) == "" {
		cfg.Repo = defaultRepo
	}
	if strings.TrimSpace(cfg.GOOS) == "" {
		cfg.GOOS = runtime.GOOS
	}
	if strings.TrimSpace(cfg.GOARCH) == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Updater{cfg: cfg, client: client}
}

func (u *Updater) Run(ctx context.Context, opts Options) (Result, error) {
	result := Result{CurrentVersion: u.cfg.CurrentVersion}
	if err := u.validatePlatform(); err != nil {
		return result, err
	}

	target, err := u.targetVersion(ctx, opts.Version)
	if err != nil {
		return result, err
	}
	result.TargetVersion = target

	current := normalizeTag(u.cfg.CurrentVersion)
	currentParsed := false
	if _, err := parseVersion(current); err == nil {
		currentParsed = true
	}
	if !currentParsed && strings.TrimSpace(opts.Version) == "" && !opts.CheckOnly {
		return result, fmt.Errorf("current build has no release version; pass --version vX.Y.Z")
	}

	if currentParsed {
		comparison, err := compareVersions(current, target)
		if err != nil {
			return result, err
		}
		if comparison > 0 && strings.TrimSpace(opts.Version) == "" {
			result.UpdateAvailable = false
			return result, nil
		}
		if comparison == 0 && !opts.Force {
			result.UpdateAvailable = false
			return result, nil
		}
	}

	result.UpdateAvailable = true
	if opts.CheckOnly {
		return result, nil
	}

	u.logf("Downloading Nectar %s (%s/%s)", target, u.cfg.GOOS, u.cfg.GOARCH)
	tmpDir, err := os.MkdirTemp("", "nectar-update-*")
	if err != nil {
		return result, fmt.Errorf("create update temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	asset := assetName(target, u.cfg.GOOS, u.cfg.GOARCH)
	baseURL := u.downloadBase(target)
	archivePath := filepath.Join(tmpDir, asset)
	if err := u.download(ctx, baseURL+"/"+asset, archivePath); err != nil {
		return result, err
	}

	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := u.download(ctx, baseURL+"/checksums.txt", checksumsPath); err != nil {
		return result, err
	}
	if err := verifyChecksum(archivePath, checksumsPath, asset); err != nil {
		return result, err
	}
	u.logf("Checksum verified")

	extractedPath := filepath.Join(tmpDir, "nectar")
	if err := extractBinary(archivePath, extractedPath); err != nil {
		return result, err
	}

	targetPath := strings.TrimSpace(u.cfg.ExecutablePath)
	if targetPath == "" {
		targetPath, err = os.Executable()
		if err != nil {
			return result, fmt.Errorf("locate current executable: %w", err)
		}
	}
	if resolved, err := filepath.EvalSymlinks(targetPath); err == nil {
		targetPath = resolved
	}
	if err := replaceBinary(extractedPath, targetPath); err != nil {
		return result, err
	}
	u.logf("Installed %s", targetPath)
	result.Updated = true
	return result, nil
}

func (u *Updater) validatePlatform() error {
	switch u.cfg.GOOS {
	case "darwin", "linux":
	default:
		return fmt.Errorf("self-update is not supported on %s", u.cfg.GOOS)
	}
	switch u.cfg.GOARCH {
	case "amd64", "arm64":
	default:
		return fmt.Errorf("self-update is not supported on %s/%s", u.cfg.GOOS, u.cfg.GOARCH)
	}
	return nil
}

func (u *Updater) targetVersion(ctx context.Context, requested string) (string, error) {
	if requested = strings.TrimSpace(requested); requested != "" {
		return normalizeTag(requested), nil
	}
	latestURL := strings.TrimSpace(u.cfg.LatestURL)
	if latestURL == "" {
		latestURL = "https://github.com/" + u.cfg.Repo + "/releases/latest"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", fmt.Errorf("create latest release request: %w", err)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch latest release: %s", resp.Status)
	}
	path := resp.Request.URL.Path
	const marker = "/releases/tag/"
	index := strings.LastIndex(path, marker)
	if index < 0 || index+len(marker) >= len(path) {
		return "", fmt.Errorf("could not determine latest release tag from %s", resp.Request.URL)
	}
	return normalizeTag(strings.TrimSuffix(path[index+len(marker):], "/")), nil
}

func (u *Updater) downloadBase(version string) string {
	if base := strings.TrimSpace(u.cfg.DownloadBase); base != "" {
		return strings.TrimRight(base, "/")
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s", u.cfg.Repo, version)
}

func (u *Updater) download(ctx context.Context, url, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", destination, err)
	}
	return nil
}

func (u *Updater) logf(format string, args ...any) {
	if u.cfg.Logf != nil {
		u.cfg.Logf(format, args...)
	}
}

func assetName(version, goos, goarch string) string {
	return fmt.Sprintf("nectar_%s_%s_%s.tar.gz", versionWithoutV(version), goos, goarch)
}

func verifyChecksum(archivePath, checksumsPath, asset string) error {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}
	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == asset {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s not found", asset)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash archive: %w", err)
	}
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	return nil
}

func extractBinary(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open archive gzip: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if filepath.Base(header.Name) != "nectar" || !header.FileInfo().Mode().IsRegular() {
			continue
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return fmt.Errorf("create extracted binary: %w", err)
		}
		if _, err := io.Copy(out, tarReader); err != nil {
			_ = out.Close()
			return fmt.Errorf("extract binary: %w", err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("close extracted binary: %w", err)
		}
		return nil
	}
	return fmt.Errorf("nectar binary not found in archive")
}

func replaceBinary(source, destination string) error {
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create binary directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".nectar-update-*")
	if err != nil {
		return fmt.Errorf("create replacement binary: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	sourceFile, err := os.Open(source)
	if err != nil {
		_ = temp.Close()
		return fmt.Errorf("open extracted binary: %w", err)
	}
	if _, err := io.Copy(temp, sourceFile); err != nil {
		_ = sourceFile.Close()
		_ = temp.Close()
		return fmt.Errorf("write replacement binary: %w", err)
	}
	if err := sourceFile.Close(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("close extracted binary: %w", err)
	}
	if err := temp.Chmod(0o755); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod replacement binary: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close replacement binary: %w", err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("replace %s: %w", destination, err)
	}
	cleanup = false
	return nil
}
