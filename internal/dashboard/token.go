package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// EnsureToken reads a dashboard token from path, creating one with 0600
// permissions when the file does not exist yet.
func EnsureToken(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("dashboard token path is empty")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("dashboard token file %s is empty", path)
		}
		_ = os.Chmod(path, 0o600)
		return token, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read dashboard token: %w", err)
	}

	token, err := newToken()
	if err != nil {
		return "", err
	}
	if err := writeToken(path, token); err != nil {
		return "", err
	}
	return token, nil
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate dashboard token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func writeToken(path, token string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create dashboard token directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".dashboard-token-*")
	if err != nil {
		return fmt.Errorf("create dashboard token: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod dashboard token: %w", err)
	}
	if _, err := temp.WriteString(token + "\n"); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write dashboard token: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync dashboard token: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close dashboard token: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace dashboard token: %w", err)
	}
	return nil
}

// LoginURL returns the one-time browser login URL for a dashboard listener.
func LoginURL(listen, token string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		host = ""
		port = ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	base := "http://" + net.JoinHostPort(host, port)
	if port == "" {
		base = "http://" + host
	}
	return base + "/login?token=" + url.QueryEscape(token)
}
