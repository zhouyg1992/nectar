package memory

import (
	"context"
	"strings"
	"time"
)

type Kind string

const (
	KindProfile Kind = "profile"
	KindProject Kind = "project"
	KindFact    Kind = "fact"
	KindLesson  Kind = "lesson"
	KindEpisode Kind = "episode"
)

type Status string

const (
	StatusCandidate Status = "candidate"
	StatusActive    Status = "active"
	StatusArchived  Status = "archived"
)

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
	ScopeChat    Scope = "chat"
)

type Locality string

const (
	LocalityPortable  Locality = "portable"
	LocalityTemplated Locality = "templated"
	LocalityDevice    Locality = "device"
)

// Memory is a portable, inspectable piece of context. It is deliberately
// engine-neutral and platform-neutral so it can be synced across devices.
type Memory struct {
	ID          string     `json:"id"`
	Kind        Kind       `json:"kind"`
	Scope       Scope      `json:"scope"`
	ScopeID     string     `json:"scope_id,omitempty"`
	ProjectID   string     `json:"project_id,omitempty"`
	DeviceID    string     `json:"device_id,omitempty"`
	Locality    Locality   `json:"locality,omitempty"`
	Content     string     `json:"content"`
	Tags        []string   `json:"tags,omitempty"`
	Source      string     `json:"source,omitempty"`
	Evidence    []string   `json:"evidence,omitempty"`
	Revision    int        `json:"revision,omitempty"`
	ContentHash string     `json:"content_hash,omitempty"`
	Confidence  float64    `json:"confidence,omitempty"`
	Pinned      bool       `json:"pinned,omitempty"`
	Status      Status     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	UseCount    int        `json:"use_count,omitempty"`
}

type SearchOptions struct {
	Query string
	// Scope optionally restricts results to one kind of scope. Empty means all
	// scopes visible through ScopeIDs.
	Scope Scope
	// ScopeIDs are the concrete scopes visible to this search. Global memories
	// are always included. Empty means every scope.
	ScopeIDs []string
	Limit    int
	Now      time.Time
}

type ListOptions struct {
	Status   Status
	Scope    Scope
	ScopeIDs []string
}

type Store interface {
	Put(ctx context.Context, item Memory) (Memory, error)
	Get(ctx context.Context, id string) (Memory, bool, error)
	List(ctx context.Context, opts ListOptions) ([]Memory, error)
	Search(ctx context.Context, opts SearchOptions) ([]Memory, error)
	SetStatus(ctx context.Context, id string, status Status) error
	SetPinned(ctx context.Context, id string, pinned bool) error
	UpdateContent(ctx context.Context, id, content string) (Memory, error)
	Touch(ctx context.Context, ids []string) error
	Delete(ctx context.Context, id string) error
	Counts(ctx context.Context, scopeIDs []string) (map[Status]int, error)
}

// ChatScope returns the portable scope id for a chat. Channel and chat id are
// platform-stable identifiers, not local paths.
func ChatScope(channel, chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	return "chat:" + strings.TrimSpace(channel) + "/" + chatID
}

// ProjectScope returns a portable project id from the workspace base name.
// Absolute paths differ across machines, so they must not be stored directly.
func ProjectScope(workdir string) string {
	projectID := ProjectID(workdir)
	if projectID == "" {
		return ""
	}
	return "project:" + projectID
}
