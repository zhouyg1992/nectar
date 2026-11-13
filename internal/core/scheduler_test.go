package core

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestScheduler(t *testing.T, now func() time.Time, withAnchor bool) (*Scheduler, *AnchorStore, SessionKey) {
	t.Helper()
	dir := t.TempDir()
	anchors, err := NewAnchorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := SessionKey{Channel: "feishu", ChatID: "chat-1", AgentID: "codex"}
	if withAnchor {
		if err := anchors.Set(key, SessionAnchor{
			Channel:  "feishu",
			ChatID:   "chat-1",
			ChatType: "group",
			UserID:   "user-1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	scheduler, err := NewScheduler(SchedulerOptions{
		StorePath:       filepath.Join(dir, "schedules.json"),
		Anchors:         anchors,
		DefaultTimezone: "UTC",
		Now:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return scheduler, anchors, key
}

func scheduleRequestFor(key SessionKey, mode, prompt string) ScheduleRequest {
	return ScheduleRequest{
		Mode:     mode,
		Cron:     "0 9 * * *",
		In:       "2h",
		Prompt:   prompt,
		Timezone: "UTC",
		Channel:  key.Channel,
		ChatID:   key.ChatID,
		AgentID:  key.AgentID,
	}
}

func TestSchedulerAddPauseResumeDelete(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	scheduler, _, key := newTestScheduler(t, clock, true)

	task, err := scheduler.Add(scheduleRequestFor(key, "cron", "daily report"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	wantNext := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	if !task.NextRun.Equal(wantNext) {
		t.Fatalf("next = %s, want %s", task.NextRun, wantNext)
	}
	if got := scheduler.List(false); len(got) != 1 {
		t.Fatalf("list = %d tasks", len(got))
	}

	if err := scheduler.Pause(task.ID); err != nil {
		t.Fatal(err)
	}
	paused, _ := scheduler.Get(task.ID)
	if paused.Status != SchedulePaused || !paused.NextRun.IsZero() {
		t.Fatalf("paused = %#v", paused)
	}

	now = now.Add(3 * time.Hour)
	if err := scheduler.Resume(task.ID); err != nil {
		t.Fatal(err)
	}
	resumed, _ := scheduler.Get(task.ID)
	if resumed.Status != ScheduleActive {
		t.Fatalf("status = %s", resumed.Status)
	}
	if !resumed.NextRun.After(now) {
		t.Fatalf("resumed next = %s, now = %s", resumed.NextRun, now)
	}

	if err := scheduler.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if got := scheduler.List(true); len(got) != 0 {
		t.Fatalf("delete left %d tasks", len(got))
	}
}

func TestSchedulerOneShotPauseResumeRemaining(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	scheduler, _, key := newTestScheduler(t, clock, true)

	task, err := scheduler.Add(scheduleRequestFor(key, "in", "reminder"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !task.ExecuteAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("execute at = %s", task.ExecuteAt)
	}

	now = now.Add(30 * time.Minute)
	if err := scheduler.Pause(task.ID); err != nil {
		t.Fatal(err)
	}
	paused, _ := scheduler.Get(task.ID)
	if paused.RemainingSeconds != int64(90*time.Minute/time.Second) {
		t.Fatalf("remaining = %d", paused.RemainingSeconds)
	}

	now = now.Add(time.Hour)
	if err := scheduler.Resume(task.ID); err != nil {
		t.Fatal(err)
	}
	resumed, _ := scheduler.Get(task.ID)
	if !resumed.ExecuteAt.Equal(now.Add(90 * time.Minute)) {
		t.Fatalf("resumed at = %s, want %s", resumed.ExecuteAt, now.Add(90*time.Minute))
	}
}

func TestSchedulerPersistsTasks(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	scheduler, anchors, key := newTestScheduler(t, clock, true)
	if _, err := scheduler.Add(scheduleRequestFor(key, "cron", "persisted"), "user-1"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewScheduler(SchedulerOptions{
		StorePath:       scheduler.storePath,
		Anchors:         anchors,
		DefaultTimezone: "UTC",
		Now:             clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	tasks := reloaded.List(true)
	if len(tasks) != 1 || tasks[0].Prompt != "persisted" {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestSchedulerRequiresDeliveryAnchor(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, false)
	if _, err := scheduler.Add(scheduleRequestFor(key, "cron", "no anchor"), "user-1"); err == nil {
		t.Fatal("expected missing anchor error")
	}
}

func TestSchedulerFiresOnceAndCompletes(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	scheduler, _, key := newTestScheduler(t, clock, true)
	fired := make(chan ScheduledTask, 1)
	scheduler.trigger = func(task ScheduledTask, anchor SessionAnchor) error {
		if anchor.ChatID != key.ChatID {
			t.Errorf("anchor chat = %s", anchor.ChatID)
		}
		fired <- task
		return nil
	}

	task, err := scheduler.Add(scheduleRequestFor(key, "in", "fire once"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Hour)
	scheduler.fireDue(now)

	select {
	case got := <-fired:
		if got.ID != task.ID {
			t.Fatalf("fired %s, want %s", got.ID, task.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task did not fire")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := scheduler.Get(task.ID)
		if current.Status == ScheduleCompleted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	current, _ := scheduler.Get(task.ID)
	t.Fatalf("final status = %s", current.Status)
}

func TestSchedulerUpdatePromptKeepsNextRun(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, true)
	task, err := scheduler.Add(scheduleRequestFor(key, "cron", "old"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := scheduler.Update(task.ID, SchedulePatch{Prompt: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Prompt != "new" {
		t.Fatalf("prompt = %q", updated.Prompt)
	}
	if !updated.NextRun.Equal(task.NextRun) {
		t.Fatalf("next changed from %s to %s", task.NextRun, updated.NextRun)
	}
}
