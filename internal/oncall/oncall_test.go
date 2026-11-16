package oncall

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestWindowContainsAndCrossesMidnight(t *testing.T) {
	window, err := ParseWindow("22:00-08:00")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		hour, minute int
		want         bool
	}{
		{23, 0, true},
		{2, 30, true},
		{8, 0, false},
		{12, 0, false},
		{21, 59, false},
	}
	for _, tc := range cases {
		value := time.Date(2026, 11, 16, tc.hour, tc.minute, 0, 0, time.Local)
		if got := window.Contains(value); got != tc.want {
			t.Fatalf("%02d:%02d Contains = %v, want %v", tc.hour, tc.minute, got, tc.want)
		}
	}
	if _, err := ParseWindow("22:00"); err == nil {
		t.Fatal("expected invalid window error")
	}
}

func TestStorePersistsAndMarksInterruptedJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oncall.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	watch := Watch{ID: "w_1", Repo: "owner/repo", Branch: "main", Status: WatchActive, CreatedAt: now, UpdatedAt: now}
	job := Job{ID: "j_1", WatchID: watch.ID, Repo: watch.Repo, Branch: watch.Branch, Status: JobRunning, CreatedAt: now}
	if err := store.AddWatch(watch); err != nil {
		t.Fatal(err)
	}
	if err := store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.GetJob(job.ID)
	if !ok || got.Status != JobRunning {
		t.Fatalf("reloaded job = %#v, %v", got, ok)
	}
	changed, err := reloaded.MarkInterruptedJobs(now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].Status != JobFailed {
		t.Fatalf("changed = %#v", changed)
	}
	if got, _ := reloaded.GetJob(job.ID); got.FinishedAt == nil || !strings.Contains(got.Error, "restarted") {
		t.Fatalf("interrupted job = %#v", got)
	}
}

func TestGitHubCLILatestFailed(t *testing.T) {
	client := &GitHubCLI{run: func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		if dir != "" || name != "gh" {
			t.Fatalf("unexpected command %q %q %#v", dir, name, args)
		}
		return []byte(`[
			{"databaseId": 7, "workflowName": "CI", "headSha": "abc", "status": "completed", "conclusion": "success", "url": "u0", "event": "push", "createdAt": "2026-11-16T01:00:00Z"},
			{"databaseId": 8, "workflowName": "CI", "headSha": "def", "status": "completed", "conclusion": "failure", "url": "u1", "event": "push", "createdAt": "2026-11-16T02:00:00Z"}
		]`), nil
	}}
	run, err := client.LatestFailed(context.Background(), "owner/repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.ID != 8 || run.Workflow != "CI" {
		t.Fatalf("run = %#v", run)
	}
}

func TestRepoFromRemote(t *testing.T) {
	cases := map[string]string{
		"https://github.com/owner/repo.git": "owner/repo",
		"git@github.com:owner/repo.git":     "owner/repo",
		"ssh://git@github.com/owner/repo":   "owner/repo",
	}
	for remote, want := range cases {
		got, err := repoFromRemote(remote)
		if err != nil {
			t.Fatalf("%s: %v", remote, err)
		}
		if got != want {
			t.Fatalf("%s = %q, want %q", remote, got, want)
		}
	}
}

type fakeResolver struct {
	resolved ResolvedRepo
	err      error
}

func (f fakeResolver) Resolve(context.Context, string, string, string) (ResolvedRepo, error) {
	return f.resolved, f.err
}

type fakeWatcher struct {
	run   *FailedRun
	err   error
	calls int
	mu    sync.Mutex
}

func (f *fakeWatcher) LatestFailed(context.Context, string, string) (*FailedRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.run, f.err
}

type fakeWorktrees struct {
	root string
}

func (f fakeWorktrees) Create(context.Context, string, string, string, string) (string, error) {
	path := filepath.Join(f.root, "wt")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func (f fakeWorktrees) Diff(context.Context, string, string) (string, error) {
	return "```diff\n+ fixed\n```", nil
}

func (f fakeWorktrees) Remove(context.Context, string, string) error { return nil }

type fakeRunner struct {
	mu     sync.Mutex
	prompt string
	opts   engine.EngineOptions
	output string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, prompt string, opts engine.EngineOptions) (RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompt = prompt
	f.opts = opts
	return RunResult{Output: f.output, ToolCalls: 2}, f.err
}

type fakeNotifier struct {
	ch chan Job
}

func (f *fakeNotifier) NotifyJob(_ context.Context, job Job) error {
	f.ch <- job
	return nil
}

func (f *fakeNotifier) NotifyReport(context.Context, Watch, Report) error { return nil }

func TestServiceChecksAndRunsIsolatedJob(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	watcher := &fakeWatcher{run: &FailedRun{
		ID: 42, Workflow: "CI", Commit: "abc123", URL: "https://example.test/run/42",
		Event: "push", CreatedAt: now,
	}}
	runner := &fakeRunner{output: "1. Root cause\nmissing dependency\n\n2. Evidence\nCI log"}
	notifier := &fakeNotifier{ch: make(chan Job, 2)}
	service, err := New(Options{
		StorePath:    filepath.Join(root, "oncall.json"),
		ArtifactRoot: filepath.Join(root, "artifacts"),
		Watcher:      watcher,
		Resolver:     fakeResolver{resolved: ResolvedRepo{Path: "/repo", Repo: "owner/repo", Branch: "main"}},
		Worktrees:    fakeWorktrees{root: filepath.Join(root, "worktrees")},
		Runner:       runner,
		Notifier:     notifier,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	watch, err := service.AddWatch(context.Background(), WatchRequest{
		ChatID: "chat-1", Channel: "feishu", Autonomy: "diagnose",
	}, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.CheckWatchNow(context.Background(), watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.RunID != 42 {
		t.Fatalf("job = %#v", job)
	}
	select {
	case finished := <-notifier.ch:
		if finished.Status != JobCompleted {
			t.Fatalf("status = %s, error = %s", finished.Status, finished.Error)
		}
		if !strings.Contains(finished.Evidence, "missing dependency") {
			t.Fatalf("evidence = %q", finished.Evidence)
		}
		if finished.ArtifactDir == "" {
			t.Fatal("artifact directory is empty")
		}
		if _, err := os.Stat(filepath.Join(finished.ArtifactDir, "report.md")); err != nil {
			t.Fatalf("report artifact: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(finished.ArtifactDir, "job.json"))
		if err != nil {
			t.Fatal(err)
		}
		var persisted Job
		if err := json.Unmarshal(data, &persisted); err != nil {
			t.Fatal(err)
		}
		if persisted.ArtifactDir != finished.ArtifactDir {
			t.Fatalf("persisted artifact dir = %q, want %q", persisted.ArtifactDir, finished.ArtifactDir)
		}
		runner.mu.Lock()
		defer runner.mu.Unlock()
		if !strings.Contains(runner.prompt, "Investigate only") {
			t.Fatalf("prompt = %q", runner.prompt)
		}
		if runner.opts.WorkingDir == "" || !strings.Contains(runner.opts.WorkingDir, "worktrees") {
			t.Fatalf("working dir = %q", runner.opts.WorkingDir)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for notification")
	}

	again, err := service.CheckWatchNow(context.Background(), watch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again == nil || again.ID != job.ID {
		t.Fatalf("deduplicated job = %#v", again)
	}
	select {
	case extra := <-notifier.ch:
		t.Fatalf("unexpected duplicate notification: %#v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}
