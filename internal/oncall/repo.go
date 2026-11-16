package oncall

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// RepoResolver validates a local checkout and derives GitHub coordinates.
type RepoResolver interface {
	Resolve(ctx context.Context, path, repo, branch string) (ResolvedRepo, error)
}

type GitRepoResolver struct {
	run commandRunner
}

func NewGitRepoResolver() *GitRepoResolver {
	return &GitRepoResolver{run: execCommand}
}

func (r *GitRepoResolver) Resolve(ctx context.Context, path, repo, branch string) (ResolvedRepo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ResolvedRepo{}, fmt.Errorf("a local git checkout is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ResolvedRepo{}, fmt.Errorf("resolve repo path: %w", err)
	}
	out, err := r.run(ctx, "", "git", "-C", abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return ResolvedRepo{}, fmt.Errorf("%s is not a git checkout: %w", abs, err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ResolvedRepo{}, fmt.Errorf("%s is not a git checkout", abs)
	}
	if strings.TrimSpace(repo) == "" {
		remote, remoteErr := r.run(ctx, "", "git", "-C", root, "remote", "get-url", "origin")
		if remoteErr != nil {
			return ResolvedRepo{}, fmt.Errorf("could not infer GitHub repo from origin; pass --repo: %w", remoteErr)
		}
		repo, err = repoFromRemote(string(remote))
		if err != nil {
			return ResolvedRepo{}, err
		}
	}
	repo, err = normalizeRepo(repo)
	if err != nil {
		return ResolvedRepo{}, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		if out, branchErr := r.run(ctx, "", "git", "-C", root, "symbolic-ref", "--short", "HEAD"); branchErr == nil {
			branch = strings.TrimSpace(string(out))
		}
	}
	if branch == "" {
		branch = "main"
	}
	return ResolvedRepo{Path: root, Repo: repo, Branch: branch}, nil
}

func normalizeRepo(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, ".git")
	value = strings.Trim(value, "/")
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("github repo must be owner/name, got %q", value)
	}
	return parts[0] + "/" + parts[1], nil
}

func repoFromRemote(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", fmt.Errorf("git origin is empty")
	}
	var path string
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil {
			return "", fmt.Errorf("parse git origin: %w", err)
		}
		path = parsed.Path
	} else if idx := strings.Index(remote, ":"); idx >= 0 {
		path = remote[idx+1:]
	} else {
		path = remote
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("could not infer owner/name from git origin %q", remote)
	}
	return normalizeRepo(strings.Join(parts[len(parts)-2:], "/"))
}
