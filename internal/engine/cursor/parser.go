package cursor

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
	Type        string                     `json:"type"`
	Subtype     string                     `json:"subtype"`
	SessionID   string                     `json:"session_id"`
	Text        string                     `json:"text"`
	Message     *rawMessage                `json:"message"`
	ToolCall    map[string]json.RawMessage `json:"tool_call"`
	Result      json.RawMessage            `json:"result"`
	Error       json.RawMessage            `json:"error"`
	IsError     bool                       `json:"is_error"`
	Usage       *rawUsage                  `json:"usage"`
	TimestampMS int64                      `json:"timestamp_ms"`
	Model       string                     `json:"model"`
}

type rawMessage struct {
	Role    string       `json:"role"`
	Content []rawContent `json:"content"`
}

type rawContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (m *rawMessage) text() string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	for _, content := range m.Content {
		if content.Type == "text" {
			b.WriteString(content.Text)
		}
	}
	return b.String()
}

type rawResult struct {
	Subtype   string    `json:"subtype"`
	IsError   bool      `json:"is_error"`
	Result    string    `json:"result"`
	SessionID string    `json:"session_id"`
	Usage     *rawUsage `json:"usage"`
}

type rawUsage struct {
	InputTokens      int `json:"inputTokens"`
	OutputTokens     int `json:"outputTokens"`
	CacheReadTokens  int `json:"cacheReadTokens"`
	CacheWriteTokens int `json:"cacheWriteTokens"`
	ReasoningTokens  int `json:"reasoningTokens"`
}

func (u *rawUsage) contextTokens() int {
	// outputTokens already includes reasoningTokens; adding both would double count.
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

func (u *rawUsage) empty() bool {
	return u.contextTokens() == 0
}

// Parse translates Cursor Agent JSONL events into engine events.
func Parse(r io.Reader, emit func(engine.Event), maxOutputBytes int) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxEventLineBytes)

	total := 0
	sessionID := ""
	deltaBuf := ""
	lastMessage := ""
	var lastCallUsage *rawUsage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		total += len(line)
		if maxOutputBytes > 0 && total > maxOutputBytes {
			return fmt.Errorf("cursor-agent output exceeded %d bytes", maxOutputBytes)
		}

		var event rawEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("parse cursor-agent event: %w", err)
		}

		if event.SessionID != "" && event.SessionID != sessionID {
			sessionID = event.SessionID
			emit(engine.Event{Type: engine.EventSessionStarted, SessionID: sessionID})
		}

		switch event.Type {
		case "usage":
			if event.Usage != nil && !event.Usage.empty() {
				lastCallUsage = event.Usage
			}
		case "assistant":
			text := event.Message.text()
			if text == "" {
				continue
			}
			if event.TimestampMS > 0 {
				if deltaBuf != "" && text == deltaBuf {
					emit(engine.Event{Type: engine.EventMessage, Text: text})
					lastMessage = text
					deltaBuf = ""
					continue
				}
				emit(engine.Event{Type: engine.EventTextDelta, Text: text})
				deltaBuf += text
				continue
			}

			deltaBuf = ""
			if text == lastMessage {
				continue
			}
			emit(engine.Event{Type: engine.EventMessage, Text: text})
			lastMessage = text
		case "tool_call":
			emitTool(event, emit)
		case "result":
			if err := emitResult(event, lastCallUsage, emit); err != nil {
				return err
			}
		case "error":
			emit(engine.Event{Type: engine.EventError, Text: authErrorMessage(errorText(event.Error, event.Text))})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read cursor-agent output: %w", err)
	}
	return nil
}

func emitResult(event rawEvent, lastCallUsage *rawUsage, emit func(engine.Event)) error {
	var result rawResult
	if len(event.Result) > 0 {
		if err := json.Unmarshal(event.Result, &result); err != nil {
			var text string
			if stringErr := json.Unmarshal(event.Result, &text); stringErr != nil {
				return fmt.Errorf("parse cursor-agent result: %w", err)
			}
			result.Result = text
		}
	}

	if event.IsError || result.IsError || (result.Subtype != "" && result.Subtype != "success") {
		text := strings.TrimSpace(result.Result)
		if text == "" {
			text = errorText(event.Error, "")
		}
		emit(engine.Event{Type: engine.EventError, Text: authErrorMessage(text)})
		return nil
	}

	meta := map[string]any{}
	turnUsage := result.Usage
	if turnUsage == nil {
		turnUsage = event.Usage
	}
	if usageMeta := cursorUsageMeta(lastCallUsage, turnUsage); usageMeta != nil {
		meta["usage"] = usageMeta["usage"]
	}
	emit(engine.Event{Type: engine.EventDone, Meta: meta})
	return nil
}

func cursorUsageMeta(contextUsage, turnUsage *rawUsage) map[string]any {
	data := map[string]int{}
	// model_call usage is the final API call; result usage aggregates the turn.
	if contextUsage != nil && !contextUsage.empty() {
		data["context_tokens"] = contextUsage.contextTokens()
	}
	if turnUsage != nil && !turnUsage.empty() {
		data["input_tokens"] = turnUsage.InputTokens
		data["cached_input_tokens"] = turnUsage.CacheReadTokens + turnUsage.CacheWriteTokens
		data["output_tokens"] = turnUsage.OutputTokens
	}
	if len(data) == 0 {
		return nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return map[string]any{"usage": string(encoded)}
}

func emitTool(event rawEvent, emit func(engine.Event)) {
	name, payload := toolPayload(event.ToolCall)
	if name == "" {
		return
	}
	var fields struct {
		Args        map[string]any  `json:"args"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(payload, &fields); err != nil {
		return
	}

	tool := strings.TrimSuffix(name, "ToolCall")
	summary := strings.TrimSpace(fields.Description)
	if summary == "" {
		summary = toolSummary(tool, fields.Args)
	}

	switch event.Subtype {
	case "started":
		emit(engine.Event{
			Type: engine.EventToolCall,
			Text: summary,
			Meta: map[string]any{
				"tool":   tool,
				"status": "started",
				"input":  fields.Args,
			},
		})
	case "completed":
		text, failed := toolResult(fields.Result)
		status := "completed"
		if failed {
			status = "error"
		}
		if text == "" && status != "error" {
			return
		}
		if text == "" {
			text = tool + " failed"
		}
		emit(engine.Event{
			Type: engine.EventToolResult,
			Text: text,
			Meta: map[string]any{"tool": tool, "status": status},
		})
	}
}

func toolPayload(call map[string]json.RawMessage) (string, json.RawMessage) {
	for name, payload := range call {
		if strings.HasSuffix(name, "ToolCall") {
			return name, payload
		}
	}
	return "", nil
}

func toolSummary(tool string, args map[string]any) string {
	if args == nil {
		return tool
	}
	for _, key := range []string{"command", "path", "filePath", "file_path"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			if key == "command" {
				return value
			}
			return tool + " " + value
		}
	}
	return tool
}

func toolResult(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var payload struct {
		Success map[string]any  `json:"success"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false
	}
	if len(payload.Error) > 0 {
		return messageFromRaw(payload.Error), true
	}
	if payload.Success == nil {
		return "", false
	}

	if code, ok := numberValue(payload.Success["exitCode"]); ok && code != 0 {
		return resultText(payload.Success), true
	}
	return resultText(payload.Success), false
}

func resultText(values map[string]any) string {
	for _, key := range []string{"stdout", "content", "message", "interleavedOutput", "diffString", "stderr"} {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func numberValue(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}

func errorText(raw json.RawMessage, fallback string) string {
	if text := messageFromRaw(raw); text != "" {
		return text
	}
	if text := strings.TrimSpace(fallback); text != "" {
		return text
	}
	return "cursor-agent reported an unknown error"
}

func messageFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var obj struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if obj.Message != "" {
		return strings.TrimSpace(obj.Message)
	}
	return strings.TrimSpace(obj.Error)
}
