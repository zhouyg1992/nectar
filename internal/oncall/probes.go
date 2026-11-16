package oncall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Prober is the generic observation layer behind an on-call shift.
type Prober interface {
	Check(ctx context.Context, shift Shift) (Observation, error)
}

type Probes struct {
	GitHub  *GitHubProbe
	Command *CommandProbe
	HTTP    *HTTPProbe
}

func NewProbes() *Probes {
	return &Probes{
		GitHub:  &GitHubProbe{run: execCommand},
		Command: &CommandProbe{},
		HTTP:    &HTTPProbe{Client: &http.Client{Timeout: 30 * time.Second}},
	}
}

func (p *Probes) Check(ctx context.Context, shift Shift) (Observation, error) {
	switch shift.Source {
	case SourceGitHub:
		return p.GitHub.Check(ctx, shift)
	case SourceCommand:
		return p.Command.Check(ctx, shift)
	case SourceHTTP:
		return p.HTTP.Check(ctx, shift)
	default:
		return Observation{}, fmt.Errorf("unsupported on-call source %q", shift.Source)
	}
}

type GitHubProbe struct {
	run commandRunner
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

func (g *GitHubProbe) Check(ctx context.Context, shift Shift) (Observation, error) {
	args := []string{
		"run", "list",
		"--repo", shift.Repo,
		"--limit", "10",
		"--json", "databaseId,workflowName,headSha,status,conclusion,url,event,createdAt",
	}
	if shift.Branch != "" {
		args = append(args, "--branch", shift.Branch)
	}
	out, err := g.run(ctx, "", "gh", args...)
	if err != nil {
		return Observation{}, fmt.Errorf("list github actions runs: %w", err)
	}
	var runs []ghRun
	if err := json.Unmarshal(out, &runs); err != nil {
		return Observation{}, fmt.Errorf("parse github actions runs: %w", err)
	}
	for _, run := range runs {
		if run.Status != "completed" || !failedConclusion(run.Conclusion) {
			continue
		}
		return Observation{
			Key:     fmt.Sprintf("github:%d", run.DatabaseID),
			Summary: fmt.Sprintf("CI failed: %s", run.WorkflowName),
			Details: fmt.Sprintf("run=%s\ncommit=%s\nevent=%s", run.URL, run.HeadSHA, run.Event),
			Alert:   true,
			Repo:    shift.Repo,
			Branch:  shift.Branch,
			Commit:  run.HeadSHA,
			RunURL:  run.URL,
		}, nil
	}
	return Observation{
		Key:     "github:ok",
		Summary: "No failed GitHub Actions run",
	}, nil
}

func failedConclusion(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "failure", "timed_out", "action_required", "startup_failure":
		return true
	default:
		return false
	}
}

type CommandProbe struct{}

func (p *CommandProbe) Check(ctx context.Context, shift Shift) (Observation, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", shift.Command)
	if strings.TrimSpace(shift.Workdir) != "" {
		cmd.Dir = shift.Workdir
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if ctx.Err() != nil {
		return Observation{}, ctx.Err()
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return Observation{}, fmt.Errorf("run command probe: %w", err)
		}
	}
	text := strings.TrimSpace(output.String())
	summary := firstLine(text)
	if summary == "" {
		summary = fmt.Sprintf("command exit %d", exitCode)
	}
	key := hashText(fmt.Sprintf("%d\x00%s", exitCode, text))
	return Observation{
		Key:     "command:" + key,
		Summary: summary,
		Details: fmt.Sprintf("exit=%d\n%s", exitCode, truncate(text, 8000)),
		Alert:   shift.AlertOnExit && exitCode != 0,
	}, nil
}

type HTTPProbe struct {
	Client *http.Client
}

func (p *HTTPProbe) Check(ctx context.Context, shift Shift) (Observation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shift.URL, nil)
	if err != nil {
		return Observation{}, fmt.Errorf("build http probe request: %w", err)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return Observation{}, fmt.Errorf("http probe: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil {
		return Observation{}, fmt.Errorf("read http probe body: %w", err)
	}
	key := hashText(fmt.Sprintf("%d\x00", resp.StatusCode) + string(body))
	return Observation{
		Key:     "http:" + key,
		Summary: "HTTP " + resp.Status,
		Details: truncate(string(body), 8000),
		Alert:   resp.StatusCode >= 400,
	}, nil
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		value = value[:idx]
	}
	return truncate(strings.TrimSpace(value), 240)
}
