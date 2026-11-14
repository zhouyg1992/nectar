package core

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScheduleAPIAddListPauseDelete(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, true)
	api := NewScheduleAPI(ScheduleAPIOptions{
		Scheduler:      scheduler,
		DefaultChannel: "feishu",
		DefaultAgent:   "codex",
	})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	body := map[string]any{
		"mode":     "cron",
		"cron":     "0 9 * * *",
		"prompt":   "daily report",
		"timezone": "UTC",
		"chat_id":  key.ChatID,
		"agent_id": key.AgentID,
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(server.URL+"/schedule", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %s", resp.Status)
	}
	var created struct {
		Task ScheduledTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Task.ID == "" || created.Task.Prompt != "daily report" {
		t.Fatalf("task = %#v", created.Task)
	}

	resp, err = http.Get(server.URL + "/schedule")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var listed struct {
		Tasks []ScheduledTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tasks) != 1 {
		t.Fatalf("tasks = %d", len(listed.Tasks))
	}

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/schedule/"+created.Task.ID+"/pause", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pause status = %s", resp.Status)
	}
	paused, _ := scheduler.Get(created.Task.ID)
	if paused.Status != SchedulePaused {
		t.Fatalf("status = %s", paused.Status)
	}

	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/schedule/"+created.Task.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %s", resp.Status)
	}
	if got := scheduler.List(true); len(got) != 0 {
		t.Fatalf("tasks after delete = %d", len(got))
	}
}

func TestScheduleAPIPreview(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, _ := newTestScheduler(t, func() time.Time { return now }, true)
	api := NewScheduleAPI(ScheduleAPIOptions{Scheduler: scheduler})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	data, _ := json.Marshal(map[string]any{
		"mode":     "cron",
		"cron":     "0 9 * * *",
		"timezone": "UTC",
	})
	resp, err := http.Post(server.URL+"/schedule/preview", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %s", resp.Status)
	}
	var preview struct {
		NextRuns []time.Time `json:"next_runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.NextRuns) != 3 {
		t.Fatalf("next runs = %v", preview.NextRuns)
	}
}

func TestScheduleAPIRejectsNonAdmin(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, true)
	api := NewScheduleAPI(ScheduleAPIOptions{
		Scheduler:  scheduler,
		AdminUsers: []string{"admin-1"},
	})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	data, _ := json.Marshal(map[string]any{
		"mode":     "cron",
		"cron":     "0 9 * * *",
		"prompt":   "daily report",
		"timezone": "UTC",
		"chat_id":  key.ChatID,
		"agent_id": key.AgentID,
	})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/schedule", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nectar-User-ID", "guest-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %s", resp.Status)
	}
}

func TestScheduleAPIRunAndCancel(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	scheduler, _, key := newTestScheduler(t, func() time.Time { return now }, true)
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
	api := NewScheduleAPI(ScheduleAPIOptions{Scheduler: scheduler})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/schedule/"+task.ID+"/run", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("run status = %s", resp.Status)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not start")
	}

	req, _ = http.NewRequest(http.MethodPost, server.URL+"/schedule/"+task.ID+"/cancel", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %s", resp.Status)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := scheduler.Get(task.ID)
		if !current.Running {
			if current.LastError != "run cancelled" {
				t.Fatalf("last error = %q", current.LastError)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("task did not stop after cancel")
}
