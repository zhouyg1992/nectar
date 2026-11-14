package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/core"
)

func TestScheduleCLIRoundTrip(t *testing.T) {
	dir := t.TempDir()
	anchors, err := core.NewAnchorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := core.SessionKey{Channel: "feishu", ChatID: "chat-1", AgentID: "codex"}
	if err := anchors.Set(key, core.SessionAnchor{Channel: "feishu", ChatID: "chat-1"}); err != nil {
		t.Fatal(err)
	}
	scheduler, err := core.NewScheduler(core.SchedulerOptions{
		StorePath:       filepath.Join(dir, "schedules.json"),
		Anchors:         anchors,
		DefaultTimezone: "UTC",
		Now:             time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "api.sock")
	api := core.NewScheduleAPI(core.ScheduleAPIOptions{
		SocketPath:     socket,
		Scheduler:      scheduler,
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
	t.Setenv("NECTAR_AGENT_ID", "codex")

	if err := scheduleCLIAdd([]string{"--cron", "0 9 * * *", "--prompt", "daily report", "--timezone", "UTC", "--timeout", "15m"}, false); err != nil {
		t.Fatal(err)
	}
	tasks := scheduler.List(false)
	if len(tasks) != 1 || tasks[0].Prompt != "daily report" {
		t.Fatalf("tasks = %#v", tasks)
	}
	if tasks[0].Timeout != "15m" {
		t.Fatalf("timeout = %q", tasks[0].Timeout)
	}
	if err := scheduleCLIList([]string{"--json"}); err != nil {
		t.Fatal(err)
	}
	if err := scheduleCLIAction("pause", []string{tasks[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := scheduleCLIAction("resume", []string{tasks[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := scheduleCLIAction("delete", []string{tasks[0].ID}); err != nil {
		t.Fatal(err)
	}
	if got := scheduler.List(true); len(got) != 0 {
		t.Fatalf("tasks after delete = %d", len(got))
	}
}
