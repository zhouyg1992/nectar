package oncall

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Watcher finds the latest failed GitHub Actions run for a branch.
type Watcher interface {
	LatestFailed(ctx context.Context, repo, branch string) (*FailedRun, error)
}

// GitHubCLI uses the authenticated `gh` CLI, so Nectar does not need a second
// GitHub token or a public webhook endpoint.
type GitHubCLI struct {
	run commandRunner
}

func NewGitHubCLI() *GitHubCLI {
	return &GitHubCLI{run: execCommand}
}

type ghRun struct {
	DatabaseID   int64  `json:"databaseId"`
	WorkflowName string `json:"workflowName"`
	HeadSHA      string `json:"headSha"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	URL          string `json:"url"`
	Event        string `json:"event"`
	CreatedAt    string `json:"createdAt"`
}

func (g *GitHubCLI) LatestFailed(ctx context.Context, repo, branch string) (*FailedRun, error) {
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	if repo == "" {
		return nil, fmt.Errorf("github repo is required")
	}
	args := []string{
		"run", "list",
		"--repo", repo,
		"--limit", "10",
		"--json", "databaseId,workflowName,headSha,status,conclusion,url,event,createdAt",
	}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	out, err := g.run(ctx, "", "gh", args...)
	if err != nil {
		return nil, fmt.Errorf("list github actions runs: %w", err)
	}
	var runs []ghRun
	if err := json.Unmarshal(out, &runs); err != nil {
		return nil, fmt.Errorf("parse github actions runs: %w", err)
	}
	for _, run := range runs {
		if run.Status != "completed" || !failedConclusion(run.Conclusion) {
			continue
		}
		created, err := time.Parse(time.RFC3339, run.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse github run created_at %q: %w", run.CreatedAt, err)
		}
		return &FailedRun{
			ID:        run.DatabaseID,
			Workflow:  run.WorkflowName,
			Commit:    run.HeadSHA,
			URL:       run.URL,
			Event:     run.Event,
			CreatedAt: created,
		}, nil
	}
	return nil, nil
}

func failedConclusion(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "failure", "timed_out", "action_required", "startup_failure":
		return true
	default:
		return false
	}
}
