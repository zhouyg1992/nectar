package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ScheduleMode string

const (
	ScheduleOnce  ScheduleMode = "once"
	ScheduleCron  ScheduleMode = "cron"
	ScheduleEvery ScheduleMode = "every"
)

type ScheduleStatus string

const (
	ScheduleActive    ScheduleStatus = "active"
	SchedulePaused    ScheduleStatus = "paused"
	ScheduleCompleted ScheduleStatus = "completed"
	ScheduleFailed    ScheduleStatus = "failed"
)

// ScheduledTask is a persisted task. A task either fires once at ExecuteAt,
// on a cron expression, or at a fixed interval anchored at AnchorAt.
type ScheduledTask struct {
	ID               string         `json:"id"`
	Mode             ScheduleMode   `json:"mode"`
	CronExpr         string         `json:"cron,omitempty"`
	Every            string         `json:"every,omitempty"`
	Timezone         string         `json:"timezone,omitempty"`
	Prompt           string         `json:"prompt"`
	Description      string         `json:"description,omitempty"`
	Key              SessionKey     `json:"session_key"`
	Status           ScheduleStatus `json:"status"`
	ExecuteAt        time.Time      `json:"execute_at,omitempty"`
	RemainingSeconds int64          `json:"remaining_seconds,omitempty"`
	AnchorAt         time.Time      `json:"anchor_at,omitempty"`
	NextRun          time.Time      `json:"next_run,omitempty"`
	LastRun          time.Time      `json:"last_run,omitempty"`
	LastError        string         `json:"last_error,omitempty"`
	RunCount         int            `json:"run_count,omitempty"`
	CreatedBy        string         `json:"created_by,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// ScheduleRequest is the platform-neutral input used by the socket API and
// the chat command handler.
type ScheduleRequest struct {
	Mode        string `json:"mode"`
	Cron        string `json:"cron,omitempty"`
	Every       string `json:"every,omitempty"`
	At          string `json:"at,omitempty"`
	In          string `json:"in,omitempty"`
	Prompt      string `json:"prompt"`
	Description string `json:"description,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Channel     string `json:"channel,omitempty"`
	ChatID      string `json:"chat_id,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
}

// SchedulePatch updates an existing task. Empty strings mean "leave as is"
// except Prompt, which can be set to a new non-empty value.
type SchedulePatch struct {
	Prompt      string `json:"prompt,omitempty"`
	Description string `json:"description,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Cron        string `json:"cron,omitempty"`
	Every       string `json:"every,omitempty"`
	At          string `json:"at,omitempty"`
	In          string `json:"in,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
}

func newScheduleID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "c_" + hex.EncodeToString(buf), nil
}

func scheduleLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "local") {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", name, err)
	}
	return loc, nil
}

func parseScheduleTime(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("time is required")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", value, loc)
	if err == nil {
		return parsed, nil
	}
	parsed, err = time.ParseInLocation("2006-01-02T15:04:05", value, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time %q (use RFC3339 or YYYY-MM-DDTHH:MM)", value)
	}
	return parsed, nil
}

func buildScheduledTask(req ScheduleRequest, now time.Time, defaultTimezone, createdBy string) (*ScheduledTask, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(req.ChatID) == "" {
		return nil, fmt.Errorf("chat_id is required")
	}

	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = defaultTimezone
	}
	loc, err := scheduleLocation(timezone)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(timezone) == "" {
		timezone = loc.String()
	}

	id, err := newScheduleID()
	if err != nil {
		return nil, fmt.Errorf("generate schedule id: %w", err)
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	switch mode {
	case "":
		switch {
		case strings.TrimSpace(req.Cron) != "":
			mode = "cron"
		case strings.TrimSpace(req.Every) != "":
			mode = "every"
		case strings.TrimSpace(req.At) != "" || strings.TrimSpace(req.In) != "":
			mode = "once"
		default:
			return nil, fmt.Errorf("schedule mode is required (cron, every, at, or in)")
		}
	case "cron":
		mode = "cron"
	case "at", "in", "once":
		mode = "once"
	case "every", "interval":
		mode = "every"
	default:
		return nil, fmt.Errorf("unsupported schedule mode %q", req.Mode)
	}

	task := &ScheduledTask{
		ID:          id,
		Timezone:    timezone,
		Prompt:      prompt,
		Description: strings.TrimSpace(req.Description),
		Key: SessionKey{
			Channel:  strings.TrimSpace(req.Channel),
			ChatID:   strings.TrimSpace(req.ChatID),
			ThreadID: strings.TrimSpace(req.ThreadID),
			AgentID:  strings.TrimSpace(req.AgentID),
		},
		Status:    ScheduleActive,
		CreatedBy: createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}

	switch mode {
	case "cron":
		spec, err := parseCronExpression(req.Cron)
		if err != nil {
			return nil, err
		}
		next, err := spec.next(now)
		if err != nil {
			return nil, err
		}
		task.Mode = ScheduleCron
		task.CronExpr = strings.TrimSpace(req.Cron)
		if spec.loc != nil {
			task.Timezone = spec.loc.String()
		}
		task.NextRun = next
	case "every":
		every, err := parseEvery(req.Every)
		if err != nil {
			return nil, err
		}
		task.Mode = ScheduleEvery
		task.Every = req.Every
		task.AnchorAt = now
		task.NextRun = now.Add(every)
	case "once":
		task.Mode = ScheduleOnce
		if strings.TrimSpace(req.In) != "" {
			delay, err := parseEvery(strings.TrimSpace(req.In))
			if err != nil || delay <= 0 {
				return nil, fmt.Errorf("invalid delay %q", req.In)
			}
			task.ExecuteAt = now.Add(delay)
		} else {
			at, err := parseScheduleTime(req.At, loc)
			if err != nil {
				return nil, err
			}
			if !at.After(now) {
				return nil, fmt.Errorf("scheduled time must be in the future")
			}
			task.ExecuteAt = at
		}
		task.NextRun = task.ExecuteAt
	default:
		return nil, fmt.Errorf("unsupported schedule mode %q", req.Mode)
	}

	if task.Key.Channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	return task, nil
}

func nextRunFor(task *ScheduledTask, now time.Time) (time.Time, error) {
	if task.Status == SchedulePaused || task.Status == ScheduleCompleted || task.Status == ScheduleFailed {
		return time.Time{}, nil
	}
	switch task.Mode {
	case ScheduleOnce:
		return task.ExecuteAt, nil
	case ScheduleCron:
		spec, err := parseCronExpression(task.CronExpr)
		if err != nil {
			return time.Time{}, err
		}
		if spec.loc == nil && task.Timezone != "" {
			loc, err := scheduleLocation(task.Timezone)
			if err != nil {
				return time.Time{}, err
			}
			spec.loc = loc
		}
		return spec.next(now)
	case ScheduleEvery:
		every, err := parseEvery(task.Every)
		if err != nil {
			return time.Time{}, err
		}
		return nextEvery(task.AnchorAt, every, now), nil
	default:
		return time.Time{}, fmt.Errorf("unknown schedule mode %q", task.Mode)
	}
}

func applySchedulePatch(task *ScheduledTask, patch SchedulePatch, now time.Time) error {
	if task.Status == ScheduleCompleted || task.Status == ScheduleFailed {
		return fmt.Errorf("completed task %s cannot be updated", task.ID)
	}

	if value := strings.TrimSpace(patch.Prompt); value != "" {
		task.Prompt = value
	}
	if value := strings.TrimSpace(patch.Description); value != "" {
		task.Description = value
	}
	reschedule := false
	if value := strings.TrimSpace(patch.Timezone); value != "" {
		if _, err := scheduleLocation(value); err != nil {
			return err
		}
		task.Timezone = value
		reschedule = true
	}

	mode := strings.ToLower(strings.TrimSpace(patch.Mode))
	if mode == "" {
		switch {
		case strings.TrimSpace(patch.Cron) != "":
			mode = "cron"
		case strings.TrimSpace(patch.Every) != "":
			mode = "every"
		case strings.TrimSpace(patch.At) != "" || strings.TrimSpace(patch.In) != "":
			mode = "once"
		}
	}
	switch mode {
	case "":
		// Keep the existing mode but refresh time-based fields below.
	case "cron":
		spec, err := parseCronExpression(patch.Cron)
		if err != nil {
			return err
		}
		next, err := spec.next(now)
		if err != nil {
			return err
		}
		task.Mode = ScheduleCron
		task.CronExpr = strings.TrimSpace(patch.Cron)
		task.Every = ""
		task.ExecuteAt = time.Time{}
		if spec.loc != nil {
			task.Timezone = spec.loc.String()
		}
		task.NextRun = next
		reschedule = true
	case "once", "at", "in":
		task.Mode = ScheduleOnce
		task.CronExpr = ""
		task.Every = ""
		task.RemainingSeconds = 0
		if strings.TrimSpace(patch.In) != "" {
			delay, err := parseEvery(strings.TrimSpace(patch.In))
			if err != nil || delay <= 0 {
				return fmt.Errorf("invalid delay %q", patch.In)
			}
			task.ExecuteAt = now.Add(delay)
		} else {
			loc, err := scheduleLocation(task.Timezone)
			if err != nil {
				return err
			}
			at, err := parseScheduleTime(patch.At, loc)
			if err != nil {
				return err
			}
			if !at.After(now) {
				return fmt.Errorf("scheduled time must be in the future")
			}
			task.ExecuteAt = at
		}
		task.NextRun = task.ExecuteAt
		reschedule = true
	case "every", "interval":
		if _, err := parseEvery(patch.Every); err != nil {
			return err
		}
		task.Mode = ScheduleEvery
		task.CronExpr = ""
		task.Every = strings.TrimSpace(patch.Every)
		task.AnchorAt = now
		task.NextRun = now.Add(mustParseEvery(patch.Every))
		reschedule = true
	default:
		return fmt.Errorf("unsupported schedule mode %q", patch.Mode)
	}

	if reschedule && task.Mode == ScheduleCron {
		next, err := nextRunFor(task, now)
		if err != nil {
			return err
		}
		task.NextRun = next
	}
	if task.Status == SchedulePaused {
		task.NextRun = time.Time{}
	}
	task.UpdatedAt = now
	return nil
}

func mustParseEvery(value string) time.Duration {
	d, _ := parseEvery(value)
	return d
}

type scheduleFile struct {
	Version int             `json:"version"`
	Tasks   []ScheduledTask `json:"tasks"`
}

func loadScheduleTasks(path string) ([]ScheduledTask, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read schedules: %w", err)
	}
	var file scheduleFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse schedules: %w", err)
	}
	if file.Version != 0 && file.Version != 1 {
		return nil, fmt.Errorf("unsupported schedules version %d", file.Version)
	}
	return file.Tasks, nil
}

func saveScheduleTasks(path string, tasks []ScheduledTask) error {
	data, err := json.MarshalIndent(scheduleFile{Version: 1, Tasks: tasks}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schedules: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create schedule directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".schedules-*.tmp")
	if err != nil {
		return fmt.Errorf("create schedule temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod schedule temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write schedules: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync schedules: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close schedule temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace schedules: %w", err)
	}
	cleanup = false
	return nil
}
