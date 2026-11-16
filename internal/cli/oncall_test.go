package cli

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/oncall"
)

type cliProber struct {
	mu           sync.Mutex
	observations []oncall.Observation
	index        int
}

func (p *cliProber) Check(context.Context, oncall.Shift) (oncall.Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.observations) == 0 {
		return oncall.Observation{}, nil
	}
	idx := p.index
	if idx >= len(p.observations) {
		idx = len(p.observations) - 1
	}
	p.index++
	return p.observations[idx], nil
}

type cliRunner struct{}

func (cliRunner) Run(context.Context, string, engine.EngineOptions) (oncall.RunResult, error) {
	return oncall.RunResult{Output: "job completed"}, nil
}

type cliNotifier struct{}

func (cliNotifier) NotifyObservation(context.Context, oncall.Shift, oncall.Observation) error {
	return nil
}

func (cliNotifier) NotifyJob(context.Context, oncall.Job) error { return nil }

func (cliNotifier) NotifyReport(context.Context, oncall.Report) error { return nil }

func TestOnCallCLIRoundTrip(t *testing.T) {
	dir := t.TempDir()
	prober := &cliProber{observations: []oncall.Observation{
		{Key: "k1", Summary: "baseline"},
		{Key: "k2", Summary: "changed", Alert: true},
	}}
	service, err := oncall.New(oncall.Options{
		StorePath:    filepath.Join(dir, "oncall.json"),
		WorktreeRoot: filepath.Join(dir, "worktrees"),
		ArtifactRoot: filepath.Join(dir, "artifacts"),
		Probes:       prober,
		Runner:       cliRunner{},
		Notifier:     cliNotifier{},
		Now:          func() time.Time { return time.Now().UTC() },
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

	if err := oncallCLIStart([]string{
		"--objective", "watch something",
		"--source", "command",
		"--command", "true",
		"--autonomy", "diagnose",
	}); err != nil {
		t.Fatal(err)
	}
	shifts := service.ListShifts(true)
	if len(shifts) != 1 {
		t.Fatalf("shifts = %#v", shifts)
	}
	id := shifts[0].ID
	if err := oncallCLIList([]string{"--json"}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLICheck([]string{id}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLICheck([]string{id}); err != nil {
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
	if err := oncallCLIReport([]string{"--shift", id, "--since", "24h", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLIShiftAction("pause", []string{id}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLIShiftAction("resume", []string{id}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLIShiftAction("stop", []string{id}); err != nil {
		t.Fatal(err)
	}
	if err := oncallCLIShiftAction("delete", []string{id}); err != nil {
		t.Fatal(err)
	}
	if got := service.ListShifts(true); len(got) != 0 {
		t.Fatalf("shifts after delete = %#v", got)
	}
}
