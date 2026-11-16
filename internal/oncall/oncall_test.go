package oncall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
)

type fakeProber struct {
	mu           sync.Mutex
	observations []Observation
	index        int
	err          error
}

func (f *fakeProber) Check(context.Context, Shift) (Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return Observation{}, f.err
	}
	if len(f.observations) == 0 {
		return Observation{}, nil
	}
	idx := f.index
	if idx >= len(f.observations) {
		idx = len(f.observations) - 1
	}
	f.index++
	return f.observations[idx], nil
}

type fakeResolver struct{}

func (fakeResolver) Resolve(context.Context, string, string, string) (ResolvedRepo, error) {
	return ResolvedRepo{Path: "/repo", Repo: "owner/repo", Branch: "main"}, nil
}

type fakeWorktrees struct{}

func (fakeWorktrees) Create(context.Context, string, string, string, string) (string, error) {
	return "/tmp/oncall-worktree", nil
}

func (fakeWorktrees) Diff(context.Context, string, string) (string, error) {
	return "```diff\n+ fixed\n```", nil
}

func (fakeWorktrees) Remove(context.Context, string, string) error { return nil }

type fakeRunner struct {
	mu     sync.Mutex
	prompt string
	opts   engine.EngineOptions
}

func (f *fakeRunner) Run(_ context.Context, prompt string, opts engine.EngineOptions) (RunResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompt = prompt
	f.opts = opts
	return RunResult{Output: "1. What happened\nchanged\n\n2. Evidence\ntest", ToolCalls: 2}, nil
}

type fakeNotifier struct {
	observations chan Observation
	jobs         chan Job
	reports      chan Report
}

func (f *fakeNotifier) NotifyObservation(_ context.Context, _ Shift, observation Observation) error {
	select {
	case f.observations <- observation:
	default:
	}
	return nil
}

func (f *fakeNotifier) NotifyJob(_ context.Context, job Job) error {
	f.jobs <- job
	return nil
}

func (f *fakeNotifier) NotifyReport(_ context.Context, report Report) error {
	f.reports <- report
	return nil
}

func TestShiftBaselineTriggersOneAgentJob(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	prober := &fakeProber{observations: []Observation{
		{Key: "k1", Summary: "first observation"},
		{Key: "k2", Summary: "changed observation", Alert: true},
	}}
	runner := &fakeRunner{}
	notifier := &fakeNotifier{
		observations: make(chan Observation, 2),
		jobs:         make(chan Job, 2),
		reports:      make(chan Report, 2),
	}
	service, err := New(Options{
		StorePath:    filepath.Join(root, "oncall.json"),
		WorktreeRoot: filepath.Join(root, "worktrees"),
		ArtifactRoot: filepath.Join(root, "artifacts"),
		Probes:       prober,
		Resolver:     fakeResolver{},
		Worktrees:    fakeWorktrees{},
		Runner:       runner,
		Notifier:     notifier,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	shift, err := service.AddShift(context.Background(), ShiftRequest{
		Objective: "watch something",
		Source:    SourceCommand,
		Command:   "true",
		Channel:   "feishu",
		ChatID:    "chat-1",
		Autonomy:  "diagnose",
	}, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.CheckShiftNow(context.Background(), shift.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job != nil {
		t.Fatal("first check should only establish a baseline")
	}
	job, err = service.CheckShiftNow(context.Background(), shift.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.TriggerSummary != "changed observation" {
		t.Fatalf("job = %#v", job)
	}
	select {
	case finished := <-notifier.jobs:
		if finished.Status != JobCompleted {
			t.Fatalf("status = %s, error = %s", finished.Status, finished.Error)
		}
		if !strings.Contains(finished.Evidence, "What happened") {
			t.Fatalf("evidence = %q", finished.Evidence)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for job notification")
	}
	updated, _ := service.GetShift(shift.ID)
	if updated.Incidents != 1 || updated.AgentRuns != 1 {
		t.Fatalf("shift counters = %#v", updated)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if !strings.Contains(runner.prompt, "watch something") {
		t.Fatalf("prompt = %q", runner.prompt)
	}
}

func TestNotifyAutonomyDoesNotStartAgent(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	prober := &fakeProber{observations: []Observation{
		{Key: "k1", Summary: "baseline"},
		{Key: "k2", Summary: "changed", Alert: true},
	}}
	notifier := &fakeNotifier{
		observations: make(chan Observation, 2),
		jobs:         make(chan Job, 2),
		reports:      make(chan Report, 2),
	}
	service, err := New(Options{
		StorePath: filepath.Join(root, "oncall.json"),
		Probes:    prober,
		Runner:    &fakeRunner{},
		Notifier:  notifier,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	shift, err := service.AddShift(context.Background(), ShiftRequest{
		Objective: "notify only", Source: SourceCommand, Command: "true",
		Channel: "feishu", ChatID: "chat-1", Autonomy: "notify",
	}, "u")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CheckShiftNow(context.Background(), shift.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CheckShiftNow(context.Background(), shift.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case obs := <-notifier.observations:
		if obs.Summary != "changed" {
			t.Fatalf("observation = %#v", obs)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for observation notification")
	}
	select {
	case job := <-notifier.jobs:
		t.Fatalf("notify autonomy started a job: %#v", job)
	default:
	}
	updated, _ := service.GetShift(shift.ID)
	if updated.AgentRuns != 0 {
		t.Fatalf("agent runs = %d", updated.AgentRuns)
	}
}

func TestShiftExpiresAndSendsReport(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	prober := &fakeProber{observations: []Observation{{Key: "k1", Summary: "baseline"}}}
	notifier := &fakeNotifier{
		observations: make(chan Observation, 2),
		jobs:         make(chan Job, 2),
		reports:      make(chan Report, 2),
	}
	service, err := New(Options{
		StorePath: filepath.Join(root, "oncall.json"),
		Probes:    prober,
		Runner:    &fakeRunner{},
		Notifier:  notifier,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	shift, err := service.AddShift(context.Background(), ShiftRequest{
		Objective: "night shift", Source: SourceCommand, Command: "true",
		Channel: "feishu", ChatID: "chat-1", Autonomy: "notify", For: "1h",
	}, "u")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CheckShiftNow(context.Background(), shift.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	service.checkDue()
	select {
	case report := <-notifier.reports:
		if report.Shift.Status != ShiftCompleted {
			t.Fatalf("report shift status = %s", report.Shift.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for report")
	}
	updated, _ := service.GetShift(shift.ID)
	if updated.Status != ShiftCompleted || updated.ReportSentAt == nil {
		t.Fatalf("completed shift = %#v", updated)
	}
}

func TestGitHubProbe(t *testing.T) {
	probe := &GitHubProbe{run: func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		if dir != "" || name != "gh" {
			t.Fatalf("unexpected command %q %q %#v", dir, name, args)
		}
		return []byte(`[
			{"databaseId": 8, "workflowName": "CI", "headSha": "def", "status": "completed", "conclusion": "failure", "url": "u1", "event": "push", "createdAt": "2026-11-16T02:00:00Z"}
		]`), nil
	}}
	obs, err := probe.Check(context.Background(), Shift{Repo: "owner/repo", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Key != "github:8" || !obs.Alert || obs.Commit != "def" {
		t.Fatalf("observation = %#v", obs)
	}
}

func TestCommandProbe(t *testing.T) {
	probe := &CommandProbe{}
	shift := Shift{Command: "printf 'hello\\n'"}
	first, err := probe.Check(context.Background(), shift)
	if err != nil {
		t.Fatal(err)
	}
	second, err := probe.Check(context.Background(), shift)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key == "" || first.Key != second.Key {
		t.Fatalf("stable keys = %q, %q", first.Key, second.Key)
	}
	if !strings.Contains(first.Summary, "hello") {
		t.Fatalf("summary = %q", first.Summary)
	}
	first, err = probe.Check(context.Background(), Shift{Command: "exit 3", AlertOnExit: true})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Alert {
		t.Fatalf("expected alert observation: %#v", first)
	}
}

func TestHTTPProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("down"))
	}))
	defer server.Close()
	probe := &HTTPProbe{Client: server.Client()}
	obs, err := probe.Check(context.Background(), Shift{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Alert || !strings.Contains(obs.Summary, "503") {
		t.Fatalf("observation = %#v", obs)
	}
}
