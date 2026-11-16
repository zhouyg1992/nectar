package engine

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
)

type EventType int

const (
	EventSessionStarted EventType = iota
	EventTextDelta
	EventMessage
	EventReasoning
	EventToolCall
	EventToolResult
	EventDone
	EventUsage
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
	case EventReasoning:
		return "reasoning"
	case EventToolCall:
		return "tool_call"
	case EventToolResult:
		return "tool_result"
	case EventDone:
		return "done"
	case EventUsage:
		return "usage"
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
	Attachments    []attachment.Local
	Env            map[string]string
}

// EnvWith merges extra into base, replacing existing values and keeping the
// result deterministic. All engine adapters use it so per-turn NECTAR_*
// context reaches the agent process without mutating the daemon's environment.
func EnvWith(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(base)+len(keys))
	for _, item := range base {
		key := item
		if idx := strings.Index(item, "="); idx >= 0 {
			key = item[:idx]
		}
		if _, replaced := extra[key]; replaced {
			continue
		}
		out = append(out, item)
	}
	for _, key := range keys {
		out = append(out, key+"="+extra[key])
	}
	return out
}

// EngineAdapter is implemented once per coding agent CLI.
type EngineAdapter interface {
	Name() string
	Start(ctx context.Context, prompt string, opts EngineOptions) (<-chan Event, error)
	Resume(ctx context.Context, sessionID string, prompt string, opts EngineOptions) (<-chan Event, error)
	Cancel(ctx context.Context, sessionID string) error
}

// ErrNoActiveTurn means the engine was asked to steer a session that no longer
// has a turn accepting input. It is a normal race, not a transport failure.
var ErrNoActiveTurn = errors.New("no active turn")

// Steerer is an optional capability for engines that can inject input into a
// turn that is already running. Engines without it simply do not support
// interactive steering.
type Steerer interface {
	Steer(ctx context.Context, sessionID string, text string) error
}
