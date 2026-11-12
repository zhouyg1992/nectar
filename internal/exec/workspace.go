package execguard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafePathChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// Workspace resolves and creates the working directory used by an engine.
type Workspace struct {
	root    string
	perChat bool
}

func NewWorkspace(root string, perChat bool) (*Workspace, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("workspace root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace root: %w", err)
	}
	return &Workspace{root: abs, perChat: perChat}, nil
}

func (w *Workspace) Root() string { return w.root }

func (w *Workspace) ForChat(chatID string) (string, error) {
	dir := w.root
	if w.perChat {
		name := safeName(chatID)
		if name == "" {
			name = "default"
		}
		dir = filepath.Join(w.root, name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create chat workspace: %w", err)
	}
	return dir, nil
}

func safeName(value string) string {
	value = unsafePathChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}
