package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
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
	scheduler.trigger = func(ctx context.Context, task ScheduledTask, anchor SessionAnchor) error {
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

func TestSchedulerWaitsForRunCompletionAndRejectsUpdate(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, true)
	started := make(chan struct{})
	release := make(chan struct{})
	scheduler.trigger = func(context.Context, ScheduledTask, SessionAnchor) error {
		close(started)
		<-release
		return nil
	}

	task, err := scheduler.Add(scheduleRequestFor(key, "cron", "long run"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	scheduler.fireDue(now)
	<-started

	if _, err := scheduler.Update(task.ID, SchedulePatch{Prompt: "changed"}); err == nil {
		t.Fatal("expected update while running to fail")
	}

	now = now.Add(24 * time.Hour)
	scheduler.fireDue(now)
	current, _ := scheduler.Get(task.ID)
	if !strings.Contains(current.LastError, "still in progress") {
		t.Fatalf("last error = %q", current.LastError)
	}

	close(release)
	current = waitForSchedulerRun(t, scheduler, task.ID, 1)
	if current.RunCount != 1 {
		t.Fatalf("run count = %d, want 1", current.RunCount)
	}
	if current.Status != ScheduleActive {
		t.Fatalf("status = %s, want active", current.Status)
	}
}

func TestSchedulerRunNowKeepsOneShotPlan(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, true)
	scheduler.trigger = func(context.Context, ScheduledTask, SessionAnchor) error { return nil }
	task, err := scheduler.Add(scheduleRequestFor(key, "in", "reminder"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	wantNext := task.NextRun
	if err := scheduler.RunNow(task.ID); err != nil {
		t.Fatal(err)
	}
	current := waitForSchedulerRun(t, scheduler, task.ID, 1)
	if current.Status != ScheduleActive {
		t.Fatalf("status = %s, want active", current.Status)
	}
	if !current.NextRun.Equal(wantNext) {
		t.Fatalf("next = %s, want original %s", current.NextRun, wantNext)
	}
}

func TestSchedulerMutationRollbackOnPersistFailure(t *testing.T) {
	newTask := func(t *testing.T) (*Scheduler, ScheduledTask) {
		t.Helper()
		now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
		scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, true)
		task, err := scheduler.Add(scheduleRequestFor(key, "cron", "original"), "user-1")
		if err != nil {
			t.Fatal(err)
		}
		return scheduler, *task
	}
	breakStore := func(t *testing.T, scheduler *Scheduler) {
		t.Helper()
		blocker := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		scheduler.storePath = filepath.Join(blocker, "schedules.json")
	}

	t.Run("pause", func(t *testing.T) {
		scheduler, before := newTask(t)
		breakStore(t, scheduler)
		if err := scheduler.Pause(before.ID); err == nil {
			t.Fatal("expected persist error")
		}
		after, _ := scheduler.Get(before.ID)
		if after.Status != before.Status || !after.NextRun.Equal(before.NextRun) {
			t.Fatalf("task changed after failed pause: %#v", after)
		}
	})

	t.Run("resume", func(t *testing.T) {
		scheduler, before := newTask(t)
		if err := scheduler.Pause(before.ID); err != nil {
			t.Fatal(err)
		}
		paused, _ := scheduler.Get(before.ID)
		breakStore(t, scheduler)
		if err := scheduler.Resume(before.ID); err == nil {
			t.Fatal("expected persist error")
		}
		after, _ := scheduler.Get(before.ID)
		if after.Status != SchedulePaused || !after.NextRun.IsZero() {
			t.Fatalf("task changed after failed resume: %#v", after)
		}
		if after.UpdatedAt != paused.UpdatedAt {
			t.Fatalf("updated_at changed from %s to %s", paused.UpdatedAt, after.UpdatedAt)
		}
	})

	t.Run("delete", func(t *testing.T) {
		scheduler, before := newTask(t)
		breakStore(t, scheduler)
		if err := scheduler.Delete(before.ID); err == nil {
			t.Fatal("expected persist error")
		}
		if _, ok := scheduler.Get(before.ID); !ok {
			t.Fatal("task was removed after failed delete")
		}
	})

	t.Run("update", func(t *testing.T) {
		scheduler, before := newTask(t)
		breakStore(t, scheduler)
		if _, err := scheduler.Update(before.ID, SchedulePatch{Prompt: "changed"}); err == nil {
			t.Fatal("expected persist error")
		}
		after, _ := scheduler.Get(before.ID)
		if after.Prompt != before.Prompt {
			t.Fatalf("prompt = %q, want %q", after.Prompt, before.Prompt)
		}
	})
}

func TestSchedulerZeroStartupGraceDoesNotCatchUp(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, err := NewScheduler(SchedulerOptions{
		StartupGrace: 0,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	task := &ScheduledTask{
		ID:        "c_once",
		Mode:      ScheduleOnce,
		Status:    ScheduleActive,
		ExecuteAt: now.Add(-time.Minute),
		NextRun:   now.Add(-time.Minute),
	}
	scheduler.tasks[task.ID] = task
	scheduler.recoverLocked(now)
	if task.Status != ScheduleCompleted {
		t.Fatalf("status = %s, want completed", task.Status)
	}
}

func TestSchedulerStartRequiresTrigger(t *testing.T) {
	scheduler, _, _ := newTestScheduler(t, time.Now, true)
	if err := scheduler.Start(context.Background(), nil); err == nil {
		t.Fatal("expected nil trigger to fail")
	}
}

func TestSchedulerPrunesFinishedHistory(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, _ := newTestScheduler(t, func() time.Time { return now }, false)
	scheduler.historyLimit = 2
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("done-%d", i)
		scheduler.tasks[id] = &ScheduledTask{
			ID:        id,
			Status:    ScheduleCompleted,
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		}
	}
	active := &ScheduledTask{ID: "active", Status: ScheduleActive, UpdatedAt: now}
	scheduler.tasks[active.ID] = active

	if err := scheduler.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if _, ok := scheduler.tasks["done-0"]; ok {
		t.Fatal("oldest finished task was not pruned")
	}
	if _, ok := scheduler.tasks["done-1"]; !ok {
		t.Fatal("newer finished task was pruned")
	}
	if _, ok := scheduler.tasks["done-2"]; !ok {
		t.Fatal("newest finished task was pruned")
	}
	if _, ok := scheduler.tasks["active"]; !ok {
		t.Fatal("active task was pruned")
	}
}

func TestSchedulerUsesRouterRunResult(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, true)
	router, _ := newTestRouter(t, &fakeEngine{fail: true}, &fakePlatform{})
	scheduler.trigger = func(ctx context.Context, task ScheduledTask, anchor SessionAnchor) error {
		return router.ExecuteScheduled(ctx, platform.Message{
			Channel:  anchor.Channel,
			ChatID:   anchor.ChatID,
			ChatType: anchor.ChatType,
			UserID:   anchor.UserID,
			Text:     task.Prompt,
			Origin:   "schedule",
		})
	}

	task, err := scheduler.Add(scheduleRequestFor(key, "in", "fail visibly"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Hour)
	scheduler.fireDue(now)
	current := waitForSchedulerRun(t, scheduler, task.ID, 1)
	if current.Status != ScheduleFailed {
		t.Fatalf("status = %s, want failed", current.Status)
	}
	if !strings.Contains(current.LastError, "boom") {
		t.Fatalf("last error = %q", current.LastError)
	}
}

func newTestSchedulerWithOptions(t *testing.T, now func() time.Time, withAnchor bool, mutate func(*SchedulerOptions)) (*Scheduler, *AnchorStore, SessionKey) {
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
	opts := SchedulerOptions{
		StorePath:       filepath.Join(dir, "schedules.json"),
		Anchors:         anchors,
		DefaultTimezone: "UTC",
		Now:             now,
	}
	if mutate != nil {
		mutate(&opts)
	}
	scheduler, err := NewScheduler(opts)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler, anchors, key
}

func TestSchedulerLimitsConcurrentRuns(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestSchedulerWithOptions(t, func() time.Time { return now }, true, func(opts *SchedulerOptions) {
		opts.MaxConcurrent = 1
	})
	started := make(chan string, 2)
	release := make(chan struct{})
	scheduler.trigger = func(ctx context.Context, task ScheduledTask, _ SessionAnchor) error {
		started <- task.ID
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	first, err := scheduler.Add(scheduleRequestFor(key, "in", "first"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := scheduler.Add(scheduleRequestFor(key, "in", "second"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Hour)
	scheduler.fireDue(now)

	var firstID string
	select {
	case firstID = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first task did not start")
	}
	select {
	case got := <-started:
		t.Fatalf("second task %s started while %s was still running", got, firstID)
	case <-time.After(100 * time.Millisecond):
	}

	release <- struct{}{}
	select {
	case got := <-started:
		if got == firstID {
			t.Fatalf("first task started twice")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second task did not start after the first finished")
	}
	release <- struct{}{}

	waitForSchedulerRun(t, scheduler, first.ID, 1)
	waitForSchedulerRun(t, scheduler, second.ID, 1)
}

func TestSchedulerRunTimeout(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestSchedulerWithOptions(t, func() time.Time { return now }, true, func(opts *SchedulerOptions) {
		opts.DefaultTimeout = 50 * time.Millisecond
	})
	scheduler.trigger = func(ctx context.Context, _ ScheduledTask, _ SessionAnchor) error {
		<-ctx.Done()
		return ctx.Err()
	}

	task, err := scheduler.Add(scheduleRequestFor(key, "in", "slow"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Hour)
	scheduler.fireDue(now)

	current := waitForSchedulerRun(t, scheduler, task.ID, 1)
	if current.Status != ScheduleFailed {
		t.Fatalf("status = %s, want failed", current.Status)
	}
	if !strings.Contains(current.LastError, "timed out after 50ms") {
		t.Fatalf("last error = %q", current.LastError)
	}
}

func TestSchedulerTaskTimeoutOverridesDefault(t *testing.T) {
	tests := []struct {
		name           string
		defaultTimeout time.Duration
		taskTimeout    string
		triggerWait    time.Duration
		wantError      string
	}{
		{name: "task timeout wins", defaultTimeout: time.Hour, taskTimeout: "40ms", wantError: "timed out after 40ms"},
		{name: "zero disables scheduler timeout", defaultTimeout: 20 * time.Millisecond, taskTimeout: "0s", triggerWait: 60 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
			scheduler, _, key := newTestSchedulerWithOptions(t, func() time.Time { return now }, true, func(opts *SchedulerOptions) {
				opts.DefaultTimeout = tt.defaultTimeout
			})
			scheduler.trigger = func(ctx context.Context, _ ScheduledTask, _ SessionAnchor) error {
				if tt.triggerWait > 0 {
					select {
					case <-time.After(tt.triggerWait):
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				<-ctx.Done()
				return ctx.Err()
			}

			req := scheduleRequestFor(key, "in", "task timeout")
			req.Timeout = tt.taskTimeout
			task, err := scheduler.Add(req, "user-1")
			if err != nil {
				t.Fatal(err)
			}
			now = now.Add(3 * time.Hour)
			scheduler.fireDue(now)

			current := waitForSchedulerRun(t, scheduler, task.ID, 1)
			if tt.wantError != "" {
				if current.Status != ScheduleFailed {
					t.Fatalf("status = %s, want failed", current.Status)
				}
				if !strings.Contains(current.LastError, tt.wantError) {
					t.Fatalf("last error = %q, want %q", current.LastError, tt.wantError)
				}
				return
			}
			if current.Status != ScheduleCompleted {
				t.Fatalf("status = %s, last error = %q", current.Status, current.LastError)
			}
		})
	}
}

func TestSchedulerCancelReportsRunningState(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestSchedulerWithOptions(t, func() time.Time { return now }, true, func(opts *SchedulerOptions) {
		opts.DefaultTimeout = time.Hour
	})
	started := make(chan struct{})
	scheduler.trigger = func(ctx context.Context, _ ScheduledTask, _ SessionAnchor) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	task, err := scheduler.Add(scheduleRequestFor(key, "cron", "long report"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	scheduler.fireDue(now)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not start")
	}

	running, _ := scheduler.Get(task.ID)
	if !running.Running {
		t.Fatal("running state was not exposed")
	}
	if err := scheduler.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}

	current := waitForSchedulerRun(t, scheduler, task.ID, 1)
	if current.Running {
		t.Fatal("task still reports running after cancel")
	}
	if current.Status != ScheduleActive {
		t.Fatalf("status = %s, want active", current.Status)
	}
	if !strings.Contains(current.LastError, "cancelled") {
		t.Fatalf("last error = %q", current.LastError)
	}
	if err := scheduler.Cancel(task.ID); err == nil {
		t.Fatal("expected cancel on an idle task to fail")
	}
}

func TestSchedulerJitterCanBeCancelledBeforeRun(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestSchedulerWithOptions(t, func() time.Time { return now }, true, func(opts *SchedulerOptions) {
		opts.Jitter = 24 * time.Hour
	})
	called := make(chan struct{}, 1)
	scheduler.trigger = func(context.Context, ScheduledTask, SessionAnchor) error {
		called <- struct{}{}
		return nil
	}

	task, err := scheduler.Add(scheduleRequestFor(key, "cron", "jittered"), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	scheduler.fireDue(now)
	select {
	case <-called:
		t.Fatal("task ignored jitter")
	case <-time.After(50 * time.Millisecond):
	}
	if err := scheduler.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	current := waitForSchedulerRun(t, scheduler, task.ID, 1)
	if !strings.Contains(current.LastError, "cancelled") {
		t.Fatalf("last error = %q", current.LastError)
	}
	select {
	case <-called:
		t.Fatal("cancelled task still reached the trigger")
	default:
	}
}

func waitForSchedulerRun(t *testing.T, scheduler *Scheduler, id string, runCount int) ScheduledTask {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		scheduler.mu.RLock()
		_, running := scheduler.running[id]
		task := *scheduler.tasks[id]
		scheduler.mu.RUnlock()
		if !running && task.RunCount >= runCount {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("schedule %s did not finish", id)
	return ScheduledTask{}
}
