package oncall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Worktrees interface {
	Create(ctx context.Context, repoPath, branch, commit, jobID string) (string, error)
	Diff(ctx context.Context, worktree, commit string) (string, error)
	Remove(ctx context.Context, repoPath, worktree string) error
}

type GitWorktrees struct {
	Root string
	run  commandRunner
}

func NewGitWorktrees(root string) *GitWorktrees {
	return &GitWorktrees{Root: root, run: execCommand}
}

var unsafeWorktreeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (g *GitWorktrees) Create(ctx context.Context, repoPath, branch, commit, jobID string) (string, error) {
	root := strings.TrimSpace(g.Root)
	if root == "" {
		return "", fmt.Errorf("on-call worktree root is empty")
	}
	name := unsafeWorktreeName.ReplaceAllString(jobID, "-")
	if name == "" {
		name = "job"
	}
	path := filepath.Join(root, name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create worktree root: %w", err)
	}
	if err := os.RemoveAll(path); err != nil {
		return "", fmt.Errorf("remove stale worktree: %w", err)
	}
	target := strings.TrimSpace(commit)
	if target == "" {
		target = strings.TrimSpace(branch)
	}
	if target == "" {
		return "", fmt.Errorf("worktree target commit or branch is required")
	}
	if _, err := g.run(ctx, "", "git", "-C", repoPath, "worktree", "add", "--detach", path, target); err == nil {
		return path, nil
	}
	if branch != "" {
		if _, fetchErr := g.run(ctx, "", "git", "-C", repoPath, "fetch", "origin", branch); fetchErr != nil {
			return "", fmt.Errorf("fetch %s: %w", branch, fetchErr)
		}
	}
	if _, err := g.run(ctx, "", "git", "-C", repoPath, "worktree", "add", "--detach", path, target); err != nil {
		return "", fmt.Errorf("create worktree at %s: %w", target, err)
	}
	return path, nil
}

func (g *GitWorktrees) Diff(ctx context.Context, worktree, commit string) (string, error) {
	// -N makes newly created files visible in git diff without staging content.
	_, _ = g.run(ctx, "", "git", "-C", worktree, "add", "-N", ".")
	stat, statErr := g.run(ctx, "", "git", "-C", worktree, "diff", "--stat")
	patch, patchErr := g.run(ctx, "", "git", "-C", worktree, "diff", "--no-ext-diff")
	if statErr != nil && patchErr != nil {
		return "", fmt.Errorf("read worktree diff: %w", patchErr)
	}
	var b strings.Builder
	if len(strings.TrimSpace(string(stat))) > 0 {
		b.WriteString("```text\n")
		b.Write(stat)
		b.WriteString("\n```\n")
	}
	if len(strings.TrimSpace(string(patch))) > 0 {
		b.WriteString("\n```diff\n")
		b.Write(patch)
		b.WriteString("\n```\n")
	}
	return truncate(b.String(), 64<<10), nil
}

func (g *GitWorktrees) Remove(ctx context.Context, repoPath, worktree string) error {
	if _, err := g.run(ctx, "", "git", "-C", repoPath, "worktree", "remove", "--force", worktree); err == nil {
		return nil
	}
	return os.RemoveAll(worktree)
}
