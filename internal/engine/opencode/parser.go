package opencode

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
	Type      string    `json:"type"`
	SessionID string    `json:"sessionID"`
	Part      *rawPart  `json:"part"`
	Error     *rawError `json:"error"`
	Message   string    `json:"message"`
}

type rawError struct {
	Message string `json:"message"`
}

type rawPart struct {
	ID        string        `json:"id"`
	PartID    string        `json:"partID"`
	SessionID string        `json:"sessionID"`
	Type      string        `json:"type"`
	Text      string        `json:"text"`
	Tool      string        `json:"tool"`
	State     *rawToolState `json:"state"`
	Tokens    *rawTokens    `json:"tokens"`
}

type rawTokens struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

type rawToolState struct {
	Status string         `json:"status"`
	Input  map[string]any `json:"input"`
	Output string         `json:"output"`
	Title  string         `json:"title"`
	Error  string         `json:"error"`
}

type parseUsage struct {
	Turn tokenUsage
	Last tokenUsage
}

// Parse translates OpenCode JSONL events into engine events.
func Parse(r io.Reader, emit func(engine.Event), maxOutputBytes int) error {
	_, err := ParseWithUsage(r, emit, maxOutputBytes)
	return err
}

// ParseWithUsage translates OpenCode JSONL and returns the last step and
// accumulated turn token usage.
func ParseWithUsage(r io.Reader, emit func(engine.Event), maxOutputBytes int) (*parseUsage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxEventLineBytes)

	total := 0
	sessionID := ""
	result := &parseUsage{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		total += len(line)
		if maxOutputBytes > 0 && total > maxOutputBytes {
			return result, fmt.Errorf("opencode output exceeded %d bytes", maxOutputBytes)
		}

		var event rawEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return result, fmt.Errorf("parse opencode event: %w", err)
		}

		eventSession := event.SessionID
		if eventSession == "" && event.Part != nil {
			eventSession = event.Part.SessionID
		}
		if eventSession != "" && eventSession != sessionID {
			sessionID = eventSession
			emit(engine.Event{Type: engine.EventSessionStarted, SessionID: sessionID})
		}

		switch event.Type {
		case "text":
			if event.Part != nil && event.Part.Text != "" {
				emit(engine.Event{Type: engine.EventMessage, Text: event.Part.Text})
			}
		case "tool_use":
			emitTool(event.Part, emit)
		case "step_finish":
			if event.Part == nil || event.Part.Tokens == nil {
				continue
			}
			tokens := tokenUsage{
				Input:     event.Part.Tokens.Input,
				Output:    event.Part.Tokens.Output,
				Reasoning: event.Part.Tokens.Reasoning,
				Cache: cacheUsage{
					Read:  event.Part.Tokens.Cache.Read,
					Write: event.Part.Tokens.Cache.Write,
				},
			}
			result.Last = tokens
			result.Turn = result.Turn.add(tokens)
		case "error":
			text := event.Message
			if event.Error != nil && event.Error.Message != "" {
				text = event.Error.Message
			}
			if text == "" {
				text = "opencode reported an unknown error"
			}
			emit(engine.Event{Type: engine.EventError, Text: text})
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read opencode output: %w", err)
	}
	return result, nil
}

func emitTool(part *rawPart, emit func(engine.Event)) {
	if part == nil || part.Tool == "" {
		return
	}
	status := ""
	output := ""
	title := ""
	input := map[string]any(nil)
	if part.State != nil {
		status = part.State.Status
		output = part.State.Output
		title = part.State.Title
		input = part.State.Input
	}

	summary := title
	if summary == "" {
		summary = part.Tool
	}
	if command, ok := input["command"].(string); ok && command != "" {
		summary = command
	} else if path, ok := input["path"].(string); ok && path != "" {
		summary = part.Tool + " " + path
	}

	emit(engine.Event{
		Type: engine.EventToolCall,
		Text: summary,
		Meta: map[string]any{
			"tool":   part.Tool,
			"status": status,
			"input":  input,
		},
	})
	if status == "completed" && output != "" {
		emit(engine.Event{
			Type: engine.EventToolResult,
			Text: output,
			Meta: map[string]any{"tool": part.Tool, "status": status},
		})
	}
	if status == "error" {
		text := output
		if text == "" {
			text = part.Tool + " failed"
		}
		emit(engine.Event{
			Type: engine.EventToolResult,
			Text: text,
			Meta: map[string]any{"tool": part.Tool, "status": status},
		})
	}
}
