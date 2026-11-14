package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultRepo = "zhouyg1992/nectar"

const (
	defaultMetadataTimeout       = 30 * time.Second
	defaultDownloadTimeout       = 10 * time.Minute
	defaultAttempts              = 2
	defaultRetryDelay            = 750 * time.Millisecond
	defaultConnectTimeout        = 15 * time.Second
	defaultTLSHandshakeTimeout   = 15 * time.Second
	defaultResponseHeaderTimeout = 30 * time.Second
)

type Config struct {
	Repo            string
	CurrentVersion  string
	GOOS            string
	GOARCH          string
	HTTPClient      *http.Client
	DownloadBase    string
	LatestURL       string
	LatestAPIURL    string
	ExecutablePath  string
	MetadataTimeout time.Duration
	DownloadTimeout time.Duration
	Attempts        int
	RetryDelay      time.Duration
	Progress        func(Progress)
	RefreshSkills   func(binaryPath string) error
	Logf            func(string, ...any)
}

type Options struct {
	Version   string
	CheckOnly bool
	Force     bool
}

type Result struct {
	CurrentVersion  string
	TargetVersion   string
	ExecutablePath  string
	UpdateAvailable bool
	Updated         bool
	SkillsUpdated   bool
}

type Progress struct {
	Asset      string
	Downloaded int64
	Total      int64
	Done       bool
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
	if cfg.MetadataTimeout <= 0 {
		cfg.MetadataTimeout = defaultMetadataTimeout
	}
	if cfg.DownloadTimeout <= 0 {
		cfg.DownloadTimeout = defaultDownloadTimeout
	}
	if cfg.Attempts <= 0 {
		cfg.Attempts = defaultAttempts
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = defaultRetryDelay
	}
	client := cfg.HTTPClient
	if client == nil {
		var transport *http.Transport
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = defaultTransport.Clone()
		} else {
			transport = &http.Transport{}
		}
		transport.Proxy = http.ProxyFromEnvironment
		transport.DialContext = (&net.Dialer{
			Timeout:   defaultConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
		transport.TLSHandshakeTimeout = defaultTLSHandshakeTimeout
		transport.ResponseHeaderTimeout = defaultResponseHeaderTimeout
		client = &http.Client{Transport: transport}
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
	if err := u.download(ctx, baseURL+"/"+asset, archivePath, asset, true); err != nil {
		return result, err
	}

	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := u.download(ctx, baseURL+"/checksums.txt", checksumsPath, "checksums.txt", false); err != nil {
		return result, err
	}
	u.logf("Verifying checksum")
	if err := verifyChecksum(archivePath, checksumsPath, asset); err != nil {
		return result, err
	}
	u.logf("Checksum verified")

	extractedPath := filepath.Join(tmpDir, "nectar")
	u.logf("Extracting binary")
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
	result.ExecutablePath = targetPath
	u.logf("Installing %s", targetPath)
	if err := replaceBinary(extractedPath, targetPath); err != nil {
		return result, err
	}
	u.logf("Installed %s", targetPath)
	if u.cfg.RefreshSkills != nil {
		u.logf("Refreshing agent skill")
		if err := u.cfg.RefreshSkills(targetPath); err != nil {
			u.logf("warning: binary updated, but agent skill refresh failed: %v", err)
		} else {
			result.SkillsUpdated = true
			u.logf("Refreshed agent skill")
		}
	}
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
	u.logf("Checking latest release")

	configuredPageURL := strings.TrimSpace(u.cfg.LatestURL)
	pageURL := configuredPageURL
	if pageURL == "" {
		pageURL = "https://github.com/" + u.cfg.Repo + "/releases/latest"
	}
	tag, pageErr := u.latestFromPage(ctx, pageURL)
	if pageErr == nil {
		return tag, nil
	}
	if configuredPageURL != "" && strings.TrimSpace(u.cfg.LatestAPIURL) == "" {
		return "", fmt.Errorf("fetch latest release: %w", pageErr)
	}

	u.logf("Release page unavailable; falling back to GitHub API")
	apiURL := strings.TrimSpace(u.cfg.LatestAPIURL)
	if apiURL == "" {
		apiURL = "https://api.github.com/repos/" + u.cfg.Repo + "/releases/latest"
	}
	tag, apiErr := u.latestFromAPI(ctx, apiURL)
	if apiErr == nil {
		return tag, nil
	}
	return "", fmt.Errorf("fetch latest release failed (release page: %v; GitHub API fallback: %v)", pageErr, apiErr)
}

func (u *Updater) downloadBase(version string) string {
	if base := strings.TrimSpace(u.cfg.DownloadBase); base != "" {
		return strings.TrimRight(base, "/")
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s", u.cfg.Repo, version)
}

func (u *Updater) latestFromPage(ctx context.Context, url string) (string, error) {
	var tag string
	err := u.retry(ctx, u.cfg.MetadataTimeout, "fetch latest release", func(attemptCtx context.Context) error {
		req, err := u.newRequest(attemptCtx, http.MethodGet, url)
		if err != nil {
			return err
		}
		resp, err := u.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if err := responseError(url, resp); err != nil {
			return err
		}
		path := resp.Request.URL.Path
		const marker = "/releases/tag/"
		index := strings.LastIndex(path, marker)
		if index < 0 || index+len(marker) >= len(path) {
			return fmt.Errorf("could not determine latest release tag from %s", resp.Request.URL)
		}
		tag = normalizeTag(strings.TrimSuffix(path[index+len(marker):], "/"))
		return nil
	})
	return tag, err
}

func (u *Updater) latestFromAPI(ctx context.Context, url string) (string, error) {
	var tag string
	err := u.retry(ctx, u.cfg.MetadataTimeout, "fetch latest release from GitHub API", func(attemptCtx context.Context) error {
		req, err := u.newRequest(attemptCtx, http.MethodGet, url)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := u.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if err := responseError(url, resp); err != nil {
			return err
		}
		var release struct {
			TagName string `json:"tag_name"`
		}
		decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
		if err := decoder.Decode(&release); err != nil {
			return fmt.Errorf("decode latest release response: %w", err)
		}
		if strings.TrimSpace(release.TagName) == "" {
			return fmt.Errorf("latest release response has no tag_name")
		}
		tag = normalizeTag(release.TagName)
		return nil
	})
	return tag, err
}

func (u *Updater) download(
	ctx context.Context,
	url string,
	destination string,
	asset string,
	showProgress bool,
) error {
	return u.retry(ctx, u.cfg.DownloadTimeout, "download "+asset, func(attemptCtx context.Context) error {
		req, err := u.newRequest(attemptCtx, http.MethodGet, url)
		if err != nil {
			return err
		}
		resp, err := u.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if err := responseError(url, resp); err != nil {
			return err
		}
		file, err := os.Create(destination)
		if err != nil {
			return fmt.Errorf("create %s: %w", destination, err)
		}
		var writer io.Writer = file
		var progress *progressWriter
		if showProgress && u.cfg.Progress != nil {
			progress = &progressWriter{
				target: file,
				asset:  asset,
				total:  resp.ContentLength,
				update: u.cfg.Progress,
			}
			writer = progress
			progress.report(false)
		}
		written, copyErr := io.Copy(writer, resp.Body)
		if progress != nil {
			progress.done(written)
		}
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("download %s: %w", asset, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", destination, closeErr)
		}
		if resp.ContentLength >= 0 && written != resp.ContentLength {
			return fmt.Errorf("download %s: received %d of %d bytes", asset, written, resp.ContentLength)
		}
		return nil
	})
}

func (u *Updater) newRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Nectar/"+normalizeTag(u.cfg.CurrentVersion))
	return req, nil
}

func (u *Updater) retry(
	ctx context.Context,
	timeout time.Duration,
	label string,
	fn func(context.Context) error,
) error {
	var lastErr error
	for attempt := 1; attempt <= u.cfg.Attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		lastErr = fn(attemptCtx)
		cancel()
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			break
		}
		if attempt >= u.cfg.Attempts || !retryable(lastErr) {
			break
		}
		u.logf("%s failed (attempt %d/%d): %v; retrying in %s",
			label, attempt, u.cfg.Attempts, lastErr, u.cfg.RetryDelay)
		timer := time.NewTimer(u.cfg.RetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("%s: %w", label, lastErr)
}

type httpStatusError struct {
	url    string
	status int
	text   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s: %s", e.url, e.text)
}

func responseError(url string, resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &httpStatusError{
		url:    url,
		status: resp.StatusCode,
		text:   resp.Status,
	}
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return false
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusRequestTimeout ||
			statusErr.status == http.StatusTooManyRequests ||
			statusErr.status >= 500
	}
	return true
}

type progressWriter struct {
	target     io.Writer
	asset      string
	total      int64
	written    int64
	lastReport time.Time
	update     func(Progress)
}

func (p *progressWriter) Write(data []byte) (int, error) {
	n, err := p.target.Write(data)
	p.written += int64(n)
	now := time.Now()
	if p.lastReport.IsZero() || now.Sub(p.lastReport) >= 200*time.Millisecond ||
		(p.total > 0 && p.written >= p.total) {
		p.report(false)
		p.lastReport = now
	}
	return n, err
}

func (p *progressWriter) done(written int64) {
	p.written = written
	p.report(true)
}

func (p *progressWriter) report(done bool) {
	p.update(Progress{
		Asset:      p.asset,
		Downloaded: p.written,
		Total:      p.total,
		Done:       done,
	})
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
