package engine

import (
	"context"
	"time"
)

type EventType int

const (
	EventSessionStarted EventType = iota
	EventTextDelta
	EventMessage
	EventToolCall
	EventToolResult
	EventDone
	EventError
)

func (t EventType) String() string {
	switch t {
	case EventSessionStarted:
		return "session_started"
	case EventTextDelta:
		return "text_delta"
	case EventMessage:
		return "message"
	case EventToolCall:
		return "tool_call"
	case EventToolResult:
		return "tool_result"
	case EventDone:
		return "done"
	case EventError:
		return "error"
	default:
		return "unknown"
	}
}

// Event is the engine-neutral stream consumed by the core layer.
type Event struct {
	Type      EventType
	Text      string
	SessionID string
	Meta      map[string]any
}

type EngineOptions struct {
	Model          string
	Effort         string
	Sandbox        string
	WorkingDir     string
	Timeout        time.Duration
	MaxOutputBytes int
}

// EngineAdapter is implemented once per coding agent CLI.
type EngineAdapter interface {
	Name() string
	Start(ctx context.Context, prompt string, opts EngineOptions) (<-chan Event, error)
	Resume(ctx context.Context, sessionID string, prompt string, opts EngineOptions) (<-chan Event, error)
	Cancel(ctx context.Context, sessionID string) error
}
