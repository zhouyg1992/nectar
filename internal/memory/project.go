package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var projectIDCache sync.Map

// ProjectID returns a stable project identity for a workspace when one can be
// derived. Git remotes survive path changes; non-git workspaces fall back to
// their base name until the user adds .nectar/project-id.
func ProjectID(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return ""
	}
	if abs, err := filepath.Abs(workdir); err == nil {
		workdir = abs
	}
	if cached, ok := projectIDCache.Load(workdir); ok {
		return cached.(string)
	}
	id := detectProjectID(workdir)
	projectIDCache.Store(workdir, id)
	return id
}

func detectProjectID(workdir string) string {
	if data, err := os.ReadFile(filepath.Join(workdir, ".nectar", "project-id")); err == nil {
		if id := sanitizeProjectID(string(data)); id != "" {
			return id
		}
	}
	root, err := gitOutput(workdir, "rev-parse", "--show-toplevel")
	if err == nil {
		root = strings.TrimSpace(root)
		rel, relErr := filepath.Rel(root, workdir)
		if relErr == nil {
			rel = filepath.ToSlash(rel)
			if rel == "." {
				rel = ""
			}
			remote, _ := gitOutput(workdir, "config", "--get", "remote.origin.url")
			remote = strings.TrimSpace(remote)
			if remote != "" {
				sum := sha256.Sum256([]byte(remote + "\x00" + rel))
				return "prj_" + hex.EncodeToString(sum[:8])
			}
		}
	}
	base := filepath.Base(workdir)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return sanitizeProjectID(base)
}

func gitOutput(workdir string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", workdir}, args...)
	out, err := exec.Command("git", cmdArgs...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func sanitizeProjectID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-._")
}

// ProjectScopeAliases includes the stable project id and the legacy
// directory-name scope so old memories remain visible after an upgrade.
func ProjectScopeAliases(workdir string) []string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return nil
	}
	var scopes []string
	if scope := ProjectScope(workdir); scope != "" {
		scopes = append(scopes, scope)
	}
	if base := sanitizeProjectID(filepath.Base(workdir)); base != "" {
		legacy := "project:" + base
		if len(scopes) == 0 || scopes[0] != legacy {
			scopes = append(scopes, legacy)
		}
	}
	return scopes
}

func DeviceID() string {
	if value := strings.TrimSpace(os.Getenv("NECTAR_DEVICE_ID")); value != "" {
		return value
	}
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostname
}

// NormalizeContent replaces known machine-specific roots with stable
// placeholders and classifies how portable the result is.
func NormalizeContent(workdir, content string) (string, Locality) {
	content = strings.TrimSpace(content)
	if workdir != "" {
		if abs, err := filepath.Abs(workdir); err == nil {
			for _, root := range []string{abs, filepath.ToSlash(abs)} {
				if root != "" {
					content = strings.ReplaceAll(content, root, "${PROJECT_ROOT}")
				}
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		for _, root := range []string{home, filepath.ToSlash(home)} {
			content = strings.ReplaceAll(content, root, "${HOME}")
		}
	}
	content = strings.ReplaceAll(content, "~/", "${HOME}/")

	locality := LocalityPortable
	if strings.Contains(content, "${") {
		locality = LocalityTemplated
	}
	if hasAbsolutePath(content) {
		locality = LocalityDevice
	}
	return content, locality
}

// ResolveContent expands known placeholders for the current device. The bool is
// false when a placeholder cannot be resolved.
func ResolveContent(workdir, content string) (string, bool) {
	resolved := true
	if strings.Contains(content, "${PROJECT_ROOT}") {
		if strings.TrimSpace(workdir) == "" {
			resolved = false
		} else {
			content = strings.ReplaceAll(content, "${PROJECT_ROOT}", workdir)
		}
	}
	if strings.Contains(content, "${HOME}") {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			content = strings.ReplaceAll(content, "${HOME}", home)
		} else {
			resolved = false
		}
	}
	return content, resolved
}

func hasAbsolutePath(content string) bool {
	for _, marker := range []string{
		"/Users/", "/home/", "/Volumes/", "/private/", "/opt/", "/var/", "/tmp/",
		`C:\`, `D:\`, `E:\`,
	} {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

// BriefHash identifies an injected memory set so a long-lived session does not
// receive the same block on every turn.
func BriefHash(items []Memory) string {
	h := sha256.New()
	for _, item := range items {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00", item.ID, item.Content, item.Locality)
	}
	return hex.EncodeToString(h.Sum(nil))
}
