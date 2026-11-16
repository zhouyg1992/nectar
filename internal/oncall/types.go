package oncall

import "time"

// Autonomy controls how far an on-call job may go.
type Autonomy string

const (
	AutonomyDiagnose Autonomy = "diagnose"
	AutonomyFix      Autonomy = "fix"
)

// WatchStatus is the lifecycle of a repository watch.
type WatchStatus string

const (
	WatchActive WatchStatus = "active"
	WatchPaused WatchStatus = "paused"
)

// JobStatus is the durable state of one on-call run.
type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCanceled  JobStatus = "canceled"
)

// Watch is a repository + branch that Nectar polls for failed GitHub Actions
// runs during a configured time window.
type Watch struct {
	ID          string      `json:"id"`
	Repo        string      `json:"repo"`
	Path        string      `json:"path"`
	Branch      string      `json:"branch"`
	Channel     string      `json:"channel"`
	ChatID      string      `json:"chat_id"`
	ChatType    string      `json:"chat_type,omitempty"`
	MessageID   string      `json:"message_id,omitempty"`
	ThreadID    string      `json:"thread_id,omitempty"`
	UserID      string      `json:"user_id,omitempty"`
	AgentID     string      `json:"agent_id,omitempty"`
	Every       string      `json:"every"`
	Window      string      `json:"window,omitempty"`
	Autonomy    Autonomy    `json:"autonomy"`
	MaxMinutes  int         `json:"max_minutes,omitempty"`
	MaxAttempts int         `json:"max_attempts,omitempty"`
	Status      WatchStatus `json:"status"`
	CreatedBy   string      `json:"created_by,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	LastChecked time.Time   `json:"last_checked,omitempty"`
	NextCheck   time.Time   `json:"next_check,omitempty"`
	LastRunID   int64       `json:"last_run_id,omitempty"`
	LastError   string      `json:"last_error,omitempty"`
	RunCount    int         `json:"run_count,omitempty"`
}

// WatchRequest is the platform-neutral input for creating a watch.
type WatchRequest struct {
	Repo        string `json:"repo,omitempty"`
	Path        string `json:"path,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Every       string `json:"every,omitempty"`
	Window      string `json:"window,omitempty"`
	Autonomy    string `json:"autonomy,omitempty"`
	MaxMinutes  int    `json:"max_minutes,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
	Channel     string `json:"channel,omitempty"`
	ChatID      string `json:"chat_id,omitempty"`
	ChatType    string `json:"chat_type,omitempty"`
	MessageID   string `json:"message_id,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
}

// FailedRun is the normalized GitHub Actions failure that starts a job.
type FailedRun struct {
	ID        int64     `json:"id"`
	Workflow  string    `json:"workflow"`
	Commit    string    `json:"commit"`
	URL       string    `json:"url"`
	Event     string    `json:"event,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Job is one isolated on-call investigation. It deliberately does not reuse a
// chat session: the worktree and context belong to the incident, not the chat.
type Job struct {
	ID          string     `json:"id"`
	WatchID     string     `json:"watch_id"`
	ParentID    string     `json:"parent_id,omitempty"`
	Attempt     int        `json:"attempt"`
	Repo        string     `json:"repo"`
	Path        string     `json:"path"`
	Branch      string     `json:"branch"`
	RunID       int64      `json:"run_id"`
	RunURL      string     `json:"run_url"`
	Workflow    string     `json:"workflow"`
	Commit      string     `json:"commit"`
	Event       string     `json:"event,omitempty"`
	Channel     string     `json:"channel"`
	ChatID      string     `json:"chat_id"`
	ChatType    string     `json:"chat_type,omitempty"`
	MessageID   string     `json:"message_id,omitempty"`
	ThreadID    string     `json:"thread_id,omitempty"`
	UserID      string     `json:"user_id,omitempty"`
	AgentID     string     `json:"agent_id,omitempty"`
	Autonomy    Autonomy   `json:"autonomy"`
	MaxMinutes  int        `json:"max_minutes,omitempty"`
	Status      JobStatus  `json:"status"`
	Summary     string     `json:"summary,omitempty"`
	Evidence    string     `json:"evidence,omitempty"`
	Diff        string     `json:"diff,omitempty"`
	Error       string     `json:"error,omitempty"`
	ArtifactDir string     `json:"artifact_dir,omitempty"`
	ToolCalls   int        `json:"tool_calls,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   time.Time  `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// RunResult is the normalized output of an engine run.
type RunResult struct {
	Output    string
	ToolCalls int
	SessionID string
}

// Report is a deterministic summary of jobs in a time range.
type Report struct {
	Since     time.Time `json:"since"`
	Until     time.Time `json:"until"`
	Total     int       `json:"total"`
	Completed int       `json:"completed"`
	Failed    int       `json:"failed"`
	Running   int       `json:"running"`
	Canceled  int       `json:"canceled"`
	Jobs      []Job     `json:"jobs"`
}

// ResolvedRepo is the local checkout and GitHub coordinates for a watch.
type ResolvedRepo struct {
	Path   string
	Repo   string
	Branch string
}
