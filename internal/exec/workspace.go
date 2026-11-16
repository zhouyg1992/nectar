package execguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafePathChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// Mode controls how a session gets its working directory.
type Mode string

const (
	ModeShared     Mode = "shared"
	ModePerChat    Mode = "per_chat"
	ModePerSession Mode = "per_session"
	ModeWorktree   Mode = "worktree"
	ModeAuto       Mode = "auto"
)

type Options struct {
	Root    string
	Mode    Mode
	DataDir string
	BotID   string
	BaseRef string
	PerChat bool
}

// Workspace resolves and creates the working directory used by an engine.
type Workspace struct {
	root         string
	projectRoot  string
	mode         Mode
	dataDir      string
	botID        string
	baseRef      string
	worktreeRoot string
}

type SessionWorkspace struct {
	Path        string
	ProjectRoot string
	Mode        Mode
}

type WorktreeInfo struct {
	Path   string
	Branch string
	Head   string
}

func NewWorkspace(root string, perChat bool) (*Workspace, error) {
	mode := ModeShared
	if perChat {
		mode = ModePerChat
	}
	return NewWorkspaceWithOptions(Options{Root: root, Mode: mode, PerChat: perChat})
}

func NewWorkspaceWithOptions(opts Options) (*Workspace, error) {
	root := strings.TrimSpace(opts.Root)
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

	mode := Mode(strings.ToLower(strings.TrimSpace(string(opts.Mode))))
	if mode == "" {
		mode = ModeShared
		if opts.PerChat {
			mode = ModePerChat
		}
	}
	switch mode {
	case ModeShared, ModePerChat, ModePerSession, ModeWorktree, ModeAuto:
	default:
		return nil, fmt.Errorf("unsupported workspace mode %q", opts.Mode)
	}

	projectRoot := abs
	if mode == ModeWorktree || mode == ModeAuto {
		gitRoot, gitErr := gitTopLevel(abs)
		if gitErr == nil {
			if _, headErr := gitOutput(abs, "rev-parse", "--verify", "HEAD"); headErr == nil {
				projectRoot = gitRoot
				if mode == ModeAuto {
					mode = ModeWorktree
				}
			} else if mode == ModeWorktree {
				return nil, fmt.Errorf("workspace %s is a git repository without a commit: %w", abs, headErr)
			} else {
				mode = ModePerChat
			}
		} else if mode == ModeWorktree {
			return nil, fmt.Errorf("workspace mode worktree requires a git repository: %w", gitErr)
		} else {
			mode = ModePerChat
		}
	}

	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir != "" {
		if absData, err := filepath.Abs(dataDir); err == nil {
			dataDir = absData
		}
	}
	botID := safeName(opts.BotID)
	if botID == "" {
		botID = "default"
	}
	baseRef := strings.TrimSpace(opts.BaseRef)
	if baseRef == "" {
		baseRef = "HEAD"
	}
	w := &Workspace{
		root:        abs,
		projectRoot: projectRoot,
		mode:        mode,
		dataDir:     dataDir,
		botID:       botID,
		baseRef:     baseRef,
	}
	if mode == ModeWorktree {
		if dataDir == "" {
			return nil, fmt.Errorf("workspace mode worktree requires a data directory")
		}
		w.worktreeRoot = filepath.Join(dataDir, "worktrees", botID)
		if err := os.MkdirAll(w.worktreeRoot, 0o755); err != nil {
			return nil, fmt.Errorf("create worktree root: %w", err)
		}
	}
	return w, nil
}

func (w *Workspace) Root() string        { return w.root }
func (w *Workspace) ProjectRoot() string { return w.projectRoot }
func (w *Workspace) Mode() Mode          { return w.mode }

func (w *Workspace) ForChat(chatID string) (string, error) {
	if w.mode != ModePerChat {
		if err := os.MkdirAll(w.root, 0o755); err != nil {
			return "", fmt.Errorf("create workspace: %w", err)
		}
		return w.root, nil
	}
	dir := filepath.Join(w.root, chatName(chatID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create chat workspace: %w", err)
	}
	return dir, nil
}

func (w *Workspace) MemoryRoot(chatID string) string {
	if w.mode == ModePerChat {
		dir, err := w.ForChat(chatID)
		if err == nil {
			return dir
		}
	}
	return w.projectRoot
}

func (w *Workspace) ForSession(sessionID, chatID string) (SessionWorkspace, error) {
	session, err := w.SessionPath(sessionID, chatID)
	if err != nil {
		return SessionWorkspace{}, err
	}
	switch w.mode {
	case ModeShared:
		if err := os.MkdirAll(w.root, 0o755); err != nil {
			return SessionWorkspace{}, fmt.Errorf("create workspace: %w", err)
		}
		return session, nil
	case ModePerChat:
		if err := os.MkdirAll(session.Path, 0o755); err != nil {
			return SessionWorkspace{}, fmt.Errorf("create chat workspace: %w", err)
		}
		return session, nil
	case ModePerSession:
		if err := os.MkdirAll(session.Path, 0o755); err != nil {
			return SessionWorkspace{}, fmt.Errorf("create session workspace: %w", err)
		}
		return session, nil
	case ModeWorktree:
		dir, err := w.ensureWorktree(sessionID)
		if err != nil {
			return SessionWorkspace{}, err
		}
		session.Path = dir
		return session, nil
	default:
		return SessionWorkspace{}, fmt.Errorf("unsupported workspace mode %q", w.mode)
	}
}

func (w *Workspace) SessionPath(sessionID, chatID string) (SessionWorkspace, error) {
	switch w.mode {
	case ModeShared:
		return SessionWorkspace{Path: w.root, ProjectRoot: w.projectRoot, Mode: w.mode}, nil
	case ModePerChat:
		dir := filepath.Join(w.root, chatName(chatID))
		return SessionWorkspace{Path: dir, ProjectRoot: dir, Mode: w.mode}, nil
	case ModePerSession:
		if strings.TrimSpace(sessionID) == "" {
			return SessionWorkspace{}, fmt.Errorf("per-session workspace requires a session id")
		}
		return SessionWorkspace{
			Path:        filepath.Join(w.root, "sessions", safeName(sessionID)),
			ProjectRoot: w.projectRoot,
			Mode:        w.mode,
		}, nil
	case ModeWorktree:
		if strings.TrimSpace(sessionID) == "" {
			return SessionWorkspace{}, fmt.Errorf("worktree workspace requires a session id")
		}
		return SessionWorkspace{
			Path:        filepath.Join(w.worktreeRoot, safeName(sessionID)),
			ProjectRoot: w.projectRoot,
			Mode:        w.mode,
		}, nil
	default:
		return SessionWorkspace{}, fmt.Errorf("unsupported workspace mode %q", w.mode)
	}
}

func (w *Workspace) ListWorktrees() ([]WorktreeInfo, error) {
	if w.mode != ModeWorktree {
		return nil, nil
	}
	output, err := gitOutput(w.projectRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var worktrees []WorktreeInfo
	var current WorktreeInfo
	flush := func() {
		if current.Path == "" {
			return
		}
		if pathWithin(current.Path, w.worktreeRoot) {
			worktrees = append(worktrees, current)
		}
		current = WorktreeInfo{}
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current.Path = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimSpace(strings.TrimPrefix(line, "HEAD "))
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "branch ")), "refs/heads/")
		case line == "":
			flush()
		}
	}
	flush()
	return worktrees, nil
}

func (w *Workspace) RemoveWorktree(sessionID string, force, deleteBranch bool) error {
	if w.mode != ModeWorktree {
		return fmt.Errorf("workspace mode is %s, not worktree", w.mode)
	}
	session, err := w.SessionPath(sessionID, "")
	if err != nil {
		return err
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, session.Path)
	if output, err := gitOutput(w.projectRoot, args...); err != nil {
		if !isWorktree(session.Path) && !pathExists(session.Path) {
			_ = w.PruneWorktrees()
			if deleteBranch {
				return w.deleteBranch("nectar/" + w.botID + "/" + safeName(sessionID))
			}
			return nil
		}
		return fmt.Errorf("remove worktree %s: %v: %s", session.Path, err, strings.TrimSpace(output))
	}
	if err := w.PruneWorktrees(); err != nil {
		return err
	}
	if deleteBranch {
		return w.deleteBranch("nectar/" + w.botID + "/" + safeName(sessionID))
	}
	return nil
}

func (w *Workspace) RemoveAllWorktrees(force, deleteBranch bool) error {
	if w.mode != ModeWorktree {
		return nil
	}
	worktrees, err := w.ListWorktrees()
	if err != nil {
		return err
	}
	var errs []error
	for _, item := range worktrees {
		args := []string{"worktree", "remove"}
		if force {
			args = append(args, "--force")
		}
		args = append(args, item.Path)
		if output, err := gitOutput(w.projectRoot, args...); err != nil {
			errs = append(errs, fmt.Errorf("remove worktree %s: %v: %s", item.Path, err, strings.TrimSpace(output)))
		} else if deleteBranch && item.Branch != "" {
			if err := w.deleteBranch(item.Branch); err != nil {
				errs = append(errs, err)
			}
		}
	}
	errs = append(errs, w.PruneWorktrees())
	return errors.Join(errs...)
}

func (w *Workspace) deleteBranch(branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil
	}
	if output, err := gitOutput(w.projectRoot, "branch", "-D", branch); err != nil {
		return fmt.Errorf("delete worktree branch %s: %v: %s", branch, err, strings.TrimSpace(output))
	}
	return nil
}

func (w *Workspace) PruneWorktrees() error {
	if w.mode != ModeWorktree {
		return nil
	}
	if output, err := gitOutput(w.projectRoot, "worktree", "prune"); err != nil {
		return fmt.Errorf("prune worktrees: %v: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func (w *Workspace) ensureWorktree(sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("worktree workspace requires a session id")
	}
	name := safeName(sessionID)
	if name == "" {
		return "", fmt.Errorf("session id %q is not a usable worktree name", sessionID)
	}
	path := filepath.Join(w.worktreeRoot, name)
	if isWorktree(path) {
		return path, nil
	}
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return "", fmt.Errorf("worktree path %s exists and is not a directory", path)
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("inspect worktree path: %w", err)
		}
		if len(entries) > 0 {
			return "", fmt.Errorf("worktree path %s exists but is not a git worktree", path)
		}
		if err := os.Remove(path); err != nil {
			return "", fmt.Errorf("remove empty worktree path: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create worktree parent: %w", err)
	}
	branch := "nectar/" + w.botID + "/" + name
	if output, err := gitOutput(w.projectRoot, "worktree", "add", "-b", branch, path, w.baseRef); err != nil {
		if retryOutput, retryErr := gitOutput(w.projectRoot, "worktree", "add", path, branch); retryErr != nil {
			return "", fmt.Errorf("create worktree %s: %v: %s; retry: %v: %s", path, err, strings.TrimSpace(output), retryErr, strings.TrimSpace(retryOutput))
		}
	}
	if !isWorktree(path) {
		return "", fmt.Errorf("git did not create a worktree at %s", path)
	}
	return path, nil
}

func isWorktree(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && !info.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pathWithin(path, dir string) bool {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func gitTopLevel(dir string) (string, error) {
	output, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func gitOutput(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}

func chatName(chatID string) string {
	name := safeName(chatID)
	if name == "" {
		return "default"
	}
	return name
}

func safeName(value string) string {
	value = unsafePathChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if len(value) > 80 {
		value = value[:80]
	}
	return value
}
