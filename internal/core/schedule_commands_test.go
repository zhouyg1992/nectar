package core

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestRouterScheduleCommandAddsAndLists(t *testing.T) {
	plat := &fakePlatform{}
	router, _ := newTestRouter(t, &fakeEngine{}, plat)

	dir := t.TempDir()
	anchors, err := NewAnchorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, err := NewScheduler(SchedulerOptions{
		StorePath:       filepath.Join(dir, "schedules.json"),
		Anchors:         anchors,
		DefaultTimezone: "UTC",
		Now:             func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	router.SetScheduling(anchors, scheduler)

	router.Handle(platform.Message{
		Channel: "feishu",
		ChatID:  "chat-1",
		UserID:  "user-1",
		Text:    "/cron in 1h 提醒我检查部署",
	})
	waitForReplies(t, plat, 1)
	if got := plat.lastReply(); !strings.Contains(got, "Task Scheduled") && !strings.Contains(got, "c_") {
		t.Fatalf("reply = %q", got)
	}

	tasks := scheduler.List(false)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	if tasks[0].Prompt != "提醒我检查部署" {
		t.Fatalf("prompt = %q", tasks[0].Prompt)
	}

	router.Handle(platform.Message{
		Channel: "feishu",
		ChatID:  "chat-1",
		UserID:  "user-1",
		Text:    "/cron list",
	})
	waitForReplies(t, plat, 2)
	if got := plat.lastReply(); !strings.Contains(got, tasks[0].ID) {
		t.Fatalf("list reply = %q", got)
	}
}

func TestRouterScheduleCommandRequiresAdmin(t *testing.T) {
	plat := &fakePlatform{}
	router, _ := newTestRouter(t, &fakeEngine{}, plat)
	router.cfg.AdminUsers = []string{"admin-1"}

	dir := t.TempDir()
	anchors, err := NewAnchorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := NewScheduler(SchedulerOptions{
		StorePath: filepath.Join(dir, "schedules.json"),
		Anchors:   anchors,
	})
	if err != nil {
		t.Fatal(err)
	}
	router.SetScheduling(anchors, scheduler)

	router.Handle(platform.Message{
		Channel: "feishu",
		ChatID:  "chat-1",
		UserID:  "guest-1",
		Text:    "/cron list",
	})
	waitForReplies(t, plat, 1)
	if got := plat.lastReply(); !strings.Contains(got, "admin users") {
		t.Fatalf("reply = %q", got)
	}
}
