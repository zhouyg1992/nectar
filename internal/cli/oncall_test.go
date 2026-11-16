package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/oncall"
)

type cliOnCallWatcher struct {
	run *oncall.FailedRun
}

func (f cliOnCallWatcher) LatestFailed(context.Context, string, string) (*oncall.FailedRun, error) {
	return f.run, nil
}

type cliOnCallResolver struct{}

func (cliOnCallResolver) Resolve(context.Context, string, string, string) (oncall.ResolvedRepo, error) {
	return oncall.ResolvedRepo{Path: "/repo", Repo: "owner/repo", Branch: "main"}, nil
}

type cliOnCallWorktrees struct {
	root string
}

func (f cliOnCallWorktrees) Create(context.Context, string, string, string, string) (string, error) {
	path := filepath.Join(f.root, "worktree")
	return path, os.MkdirAll(path, 0o700)
}

func (f cliOnCallWorktrees) Diff(context.Context, string, string) (string, error) {
	return "", nil
}

func (f cliOnCallWorktrees) Remove(context.Context, string, string) error { return nil }

type cliOnCallRunner struct{}

func (cliOnCallRunner) Run(context.Context, string, engine.EngineOptions) (oncall.RunResult, error) {
	return oncall.RunResult{Output: "root cause: test failed"}, nil
}

type cliOnCallNotifier struct{}

func (cliOnCallNotifier) NotifyJob(context.Context, oncall.Job) error { return nil }

func (cliOnCallNotifier) NotifyReport(context.Context, oncall.Watch, oncall.Report) error {
	return nil
}

func TestOnCallCLIRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	service, err := oncall.New(oncall.Options{
		StorePath:    filepath.Join(dir, "oncall.json"),
		ArtifactRoot: filepath.Join(dir, "artifacts"),
		Watcher: cliOnCallWatcher{run: &oncall.FailedRun{
			ID: 9, Workflow: "CI", Commit: "abc", URL: "https://example.test/9", CreatedAt: now.Add(time.Second),
		}},
		Resolver:  cliOnCallResolver{},
		Worktrees: cliOnCallWorktrees{root: filepath.Join(dir, "worktrees")},
		Runner:    cliOnCallRunner{},
		Notifier:  cliOnCallNotifier{},
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	socket := filepath.Join(dir, "api.sock")
	api := core.NewScheduleAPI(core.ScheduleAPIOptions{
		SocketPath:     socket,
		OnCall:         service,
		DefaultChannel: "feishu",
		DefaultAgent:   "codex",
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := api.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer api.Close()

	t.Setenv("NECTAR_API_SOCKET", socket)
	t.Setenv("NECTAR_CHANNEL", "feishu")
	t.Setenv("NECTAR_CHAT_ID", "chat-1")
	t.Setenv("NECTAR_WORKING_DIR", "/repo")

	if err := oncallCLIWatchAdd([]string{"--repo", "owner/repo", "--branch", "main", "--autonomy", "diagnose"}); err != nil {
		t.Fatal(err)
	}
	watches := service.ListWatches(true)
	if len(watches) != 1 {
		t.Fatalf("watches = %#v", watches)
	}
	if err := oncallCLIWatchList([]string{"--json"}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLIWatchAction("check", []string{watches[0].ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if jobs := service.ListJobs(10); len(jobs) == 1 && jobs[0].Status == oncall.JobCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	jobs := service.ListJobs(10)
	if len(jobs) != 1 || jobs[0].Status != oncall.JobCompleted {
		t.Fatalf("jobs = %#v", jobs)
	}
	if err := oncallCLIJobs([]string{"--json"}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLIJob([]string{jobs[0].ID, "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLIReport([]string{"--since", "24h", "--json"}); err != nil {
		t.Fatal(err)
	}
}
