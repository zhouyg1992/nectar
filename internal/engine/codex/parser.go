package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/zhouyg1992/nectar/internal/engine"
)

const maxEventLineBytes = 4 << 20

type rawEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Message  string          `json:"message"`
	Error    *rawError       `json:"error"`
	Item     *rawItem        `json:"item"`
	Usage    json.RawMessage `json:"usage"`
	Payload  *rawPayload     `json:"payload"`
}

type rawError struct {
	Message string `json:"message"`
}

type rawPayload struct {
	Type string        `json:"type"`
	Info *rawTokenInfo `json:"info"`
}

type rawTokenInfo struct {
	LastTokenUsage     *rawTokenUsage `json:"last_token_usage"`
	ModelContextWindow int            `json:"model_context_window"`
}

type rawItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	Text             string `json:"text"`
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
	Status           string `json:"status"`
	Phase            string `json:"phase"`
	ExitCode         *int   `json:"exit_code"`
}

// Parse translates Codex JSONL events into engine events.
// It returns an error for malformed JSONL so protocol drift is visible.
func Parse(r io.Reader, emit func(engine.Event), maxOutputBytes int) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxEventLineBytes)

	total := 0
	var lastUsage *rawTokenUsage
	contextWindow := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		total += len(line)
		if maxOutputBytes > 0 && total > maxOutputBytes {
			return fmt.Errorf("codex output exceeded %d bytes", maxOutputBytes)
		}

		var ev rawEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return fmt.Errorf("parse codex event: %w", err)
		}

		switch ev.Type {
		case "thread.started":
			emit(engine.Event{Type: engine.EventSessionStarted, SessionID: ev.ThreadID})
		case "event_msg":
			if ev.Payload == nil || ev.Payload.Type != "token_count" || ev.Payload.Info == nil {
				continue
			}
			if ev.Payload.Info.LastTokenUsage != nil && !ev.Payload.Info.LastTokenUsage.empty() {
				lastUsage = ev.Payload.Info.LastTokenUsage
			}
			if ev.Payload.Info.ModelContextWindow > 0 {
				contextWindow = ev.Payload.Info.ModelContextWindow
			}
		case "turn.completed":
			meta := map[string]any{}
			if usageMeta := codexUsageMeta(lastUsage, nil); usageMeta != nil {
				meta["usage"] = usageMeta["usage"]
			}
			if contextWindow > 0 {
				meta["context_window"] = contextWindow
			}
			emit(engine.Event{Type: engine.EventDone, Meta: meta})
		case "turn.failed":
			emit(engine.Event{Type: engine.EventError, Text: errorMessage(ev)})
		case "error":
			emit(engine.Event{Type: engine.EventError, Text: errorMessage(ev)})
		case "item.started":
			if ev.Item != nil && ev.Item.Type == "command_execution" {
				emit(engine.Event{
					Type: engine.EventToolCall,
					Text: ev.Item.Command,
					Meta: map[string]any{"item_id": ev.Item.ID, "tool": "command_execution"},
				})
			}
		case "item.completed":
			if ev.Item == nil {
				continue
			}
			switch ev.Item.Type {
			case "agent_message":
				if ev.Item.Text != "" {
					meta := map[string]any{}
					if ev.Item.Phase != "" {
						meta["phase"] = ev.Item.Phase
					}
					emit(engine.Event{Type: engine.EventMessage, Text: ev.Item.Text, Meta: meta})
				}
			case "command_execution":
				meta := map[string]any{
					"item_id": ev.Item.ID,
					"tool":    "command_execution",
					"command": ev.Item.Command,
					"status":  ev.Item.Status,
				}
				if ev.Item.ExitCode != nil {
					meta["exit_code"] = *ev.Item.ExitCode
				}
				emit(engine.Event{
					Type: engine.EventToolResult,
					Text: ev.Item.AggregatedOutput,
					Meta: meta,
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read codex output: %w", err)
	}
	return nil
}

func errorMessage(ev rawEvent) string {
	if ev.Error != nil && ev.Error.Message != "" {
		return ev.Error.Message
	}
	if ev.Message != "" {
		return ev.Message
	}
	return "codex reported an unknown error"
}
