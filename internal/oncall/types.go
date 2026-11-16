package oncall

import "time"

const (
	SourceGitHub  = "github"
	SourceCommand = "command"
	SourceHTTP    = "http"
)

type Autonomy string

const (
	AutonomyNotify   Autonomy = "notify"
	AutonomyDiagnose Autonomy = "diagnose"
	AutonomyFix      Autonomy = "fix"
)

type ShiftStatus string

const (
	ShiftActive    ShiftStatus = "active"
	ShiftPaused    ShiftStatus = "paused"
	ShiftCompleted ShiftStatus = "completed"
)

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCanceled  JobStatus = "canceled"
)

// Shift is one durable on-call assignment. The agent supplies the objective;
// the daemon owns the check loop, budget, state, and reporting.
type Shift struct {
	ID        string `json:"id"`
	Objective string `json:"objective"`
	Source    string `json:"source"`

	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	Path   string `json:"path,omitempty"`

	Command     string `json:"command,omitempty"`
	Workdir     string `json:"workdir,omitempty"`
	URL         string `json:"url,omitempty"`
	ActionDir   string `json:"action_dir,omitempty"`
	AlertOnExit bool   `json:"alert_on_exit,omitempty"`

	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	ChatType  string `json:"chat_type,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`

	Every        string    `json:"every"`
	StartAt      time.Time `json:"start_at"`
	EndAt        time.Time `json:"end_at,omitempty"`
	Autonomy     Autonomy  `json:"autonomy"`
	MaxMinutes   int       `json:"max_minutes,omitempty"`
	MaxAgentRuns int       `json:"max_agent_runs,omitempty"`

	Status       ShiftStatus `json:"status"`
	Checks       int         `json:"checks,omitempty"`
	Incidents    int         `json:"incidents,omitempty"`
	AgentRuns    int         `json:"agent_runs,omitempty"`
	LastKey      string      `json:"last_key,omitempty"`
	LastAlert    bool        `json:"last_alert,omitempty"`
	LastSummary  string      `json:"last_summary,omitempty"`
	LastDetails  string      `json:"last_details,omitempty"`
	LastChecked  time.Time   `json:"last_checked,omitempty"`
	NextCheck    time.Time   `json:"next_check,omitempty"`
	LastError    string      `json:"last_error,omitempty"`
	CreatedBy    string      `json:"created_by,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	CompletedAt  *time.Time  `json:"completed_at,omitempty"`
	ReportSentAt *time.Time  `json:"report_sent_at,omitempty"`
}

type ShiftRequest struct {
	Objective string `json:"objective"`
	Source    string `json:"source"`

	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	Path   string `json:"path,omitempty"`

	Command     string `json:"command,omitempty"`
	Workdir     string `json:"workdir,omitempty"`
	URL         string `json:"url,omitempty"`
	ActionDir   string `json:"action_dir,omitempty"`
	AlertOnExit bool   `json:"alert_on_exit,omitempty"`

	Every        string `json:"every,omitempty"`
	Start        string `json:"start,omitempty"`
	For          string `json:"for,omitempty"`
	Until        string `json:"until,omitempty"`
	Autonomy     string `json:"autonomy,omitempty"`
	MaxMinutes   int    `json:"max_minutes,omitempty"`
	MaxAgentRuns int    `json:"max_agent_runs,omitempty"`

	Channel   string `json:"channel,omitempty"`
	ChatID    string `json:"chat_id,omitempty"`
	ChatType  string `json:"chat_type,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
}

type Observation struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Details string `json:"details,omitempty"`
	Alert   bool   `json:"alert,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Commit  string `json:"commit,omitempty"`
	RunURL  string `json:"run_url,omitempty"`
}

// Job is one bounded action caused by a shift observation.
type Job struct {
	ID       string `json:"id"`
	ShiftID  string `json:"shift_id"`
	ParentID string `json:"parent_id,omitempty"`
	Attempt  int    `json:"attempt"`

	Objective      string `json:"objective"`
	Source         string `json:"source"`
	TriggerKey     string `json:"trigger_key,omitempty"`
	TriggerSummary string `json:"trigger_summary,omitempty"`
	TriggerDetails string `json:"trigger_details,omitempty"`
	WorkspacePath  string `json:"workspace_path,omitempty"`
	Repo           string `json:"repo,omitempty"`
	Branch         string `json:"branch,omitempty"`
	Commit         string `json:"commit,omitempty"`
	RunURL         string `json:"run_url,omitempty"`

	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	ChatType  string `json:"chat_type,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`

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

type RunResult struct {
	Output    string
	ToolCalls int
	SessionID string
}

type Report struct {
	Shift     Shift     `json:"shift"`
	Since     time.Time `json:"since"`
	Until     time.Time `json:"until"`
	Total     int       `json:"total"`
	Completed int       `json:"completed"`
	Failed    int       `json:"failed"`
	Running   int       `json:"running"`
	Canceled  int       `json:"canceled"`
	Incidents int       `json:"incidents"`
	Jobs      []Job     `json:"jobs"`
}

type ResolvedRepo struct {
	Path   string
	Repo   string
	Branch string
}
