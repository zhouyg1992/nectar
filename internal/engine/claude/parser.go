package claude

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
	Type       string                   `json:"type"`
	Subtype    string                   `json:"subtype"`
	SessionID  string                   `json:"session_id"`
	Result     string                   `json:"result"`
	IsError    bool                     `json:"is_error"`
	Message    *rawMessage              `json:"message"`
	Stream     *rawStreamEvent          `json:"event"`
	Usage      *rawUsage                `json:"usage"`
	ModelUsage map[string]rawModelUsage `json:"modelUsage"`
	Error      json.RawMessage          `json:"error"`
}

type rawStreamEvent struct {
	Type    string      `json:"type"`
	Message *rawMessage `json:"message"`
	Delta   *rawDelta   `json:"delta"`
	Usage   *rawUsage   `json:"usage"`
}

type rawDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type rawMessage struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Content []rawContent `json:"content"`
	Usage   *rawUsage    `json:"usage"`
}

type rawContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u *rawUsage) inputTotal() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

func (u *rawUsage) contextTotal() int {
	return u.inputTotal() + u.OutputTokens
}

func (u *rawUsage) empty() bool {
	return u.contextTotal() == 0
}

type rawModelUsage struct {
	InputTokens              int `json:"inputTokens"`
	OutputTokens             int `json:"outputTokens"`
	CacheReadInputTokens     int `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
	ContextWindow            int `json:"contextWindow"`
}

// Parse translates Claude Code stream-json events into engine events.
// It returns an error for malformed JSONL so protocol drift is visible.
func Parse(r io.Reader, emit func(engine.Event), maxOutputBytes int) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxEventLineBytes)

	total := 0
	sessionID := ""
	lastMessage := ""
	emitMessage := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if strings.TrimSpace(text) == strings.TrimSpace(lastMessage) {
			return
		}
		lastMessage = text
		emit(engine.Event{Type: engine.EventMessage, Text: text})
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		total += len(line)
		if maxOutputBytes > 0 && total > maxOutputBytes {
			return fmt.Errorf("claude output exceeded %d bytes", maxOutputBytes)
		}

		var event rawEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("parse claude event: %w", err)
		}

		if event.SessionID != "" && event.SessionID != sessionID {
			sessionID = event.SessionID
			emit(engine.Event{Type: engine.EventSessionStarted, SessionID: sessionID})
		}

		switch event.Type {
		case "system":
			// The init event carries metadata (model, tools, cwd) but no output.
		case "stream_event":
			if event.Stream == nil {
				continue
			}
			if event.Stream.Delta != nil &&
				event.Stream.Delta.Type == "text_delta" &&
				event.Stream.Delta.Text != "" {
				emit(engine.Event{Type: engine.EventTextDelta, Text: event.Stream.Delta.Text})
			}
		case "assistant":
			emitAssistant(event.Message, emitMessage, emit)
		case "user":
			emitToolResults(event.Message, emit)
		case "result":
			emitResult(event, emitMessage, emit)
		case "error":
			emit(engine.Event{Type: engine.EventError, Text: errorText(event)})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read claude output: %w", err)
	}
	return nil
}

func emitAssistant(message *rawMessage, emitMessage func(string), emit func(engine.Event)) {
	if message == nil {
		return
	}

	var text strings.Builder
	flushText := func() {
		if value := text.String(); value != "" {
			emitMessage(value)
			text.Reset()
		}
	}
	for _, content := range message.Content {
		switch content.Type {
		case "text":
			text.WriteString(content.Text)
		case "tool_use":
			flushText()
			emitToolCall(content, emit)
		}
	}
	flushText()
}

func emitToolCall(content rawContent, emit func(engine.Event)) {
	tool := strings.TrimSpace(content.Name)
	if tool == "" {
		tool = "tool"
	}
	args := decodeObject(content.Input)
	emit(engine.Event{
		Type: engine.EventToolCall,
		Text: toolSummary(tool, args),
		Meta: map[string]any{
			"tool":        tool,
			"tool_use_id": content.ID,
			"input":       args,
		},
	})
}

func emitToolResults(message *rawMessage, emit func(engine.Event)) {
	if message == nil {
		return
	}
	for _, content := range message.Content {
		if content.Type != "tool_result" {
			continue
		}
		text := toolResultText(content.Content)
		status := "completed"
		if content.IsError {
			status = "error"
		}
		if text == "" && status != "error" {
			continue
		}
		if text == "" {
			text = "tool failed"
		}
		emit(engine.Event{
			Type: engine.EventToolResult,
			Text: text,
			Meta: map[string]any{
				"tool_use_id": content.ToolUseID,
				"status":      status,
			},
		})
	}
}

func emitResult(event rawEvent, emitMessage func(string), emit func(engine.Event)) {
	if event.IsError || (event.Subtype != "" && event.Subtype != "success") {
		text := strings.TrimSpace(event.Result)
		if text == "" {
			text = errorText(event)
		}
		if text == "" {
			text = "claude reported " + event.Subtype
		}
		emit(engine.Event{Type: engine.EventError, Text: text})
		return
	}

	if text := strings.TrimSpace(event.Result); text != "" {
		emitMessage(text)
	}

	meta := map[string]any{}
	usage := event.Usage
	usageFromModels, contextWindow := modelUsageSummary(event.ModelUsage)
	if usage == nil || usage.empty() {
		usage = usageFromModels
	}
	if usage != nil && !usage.empty() {
		if data, err := json.Marshal(map[string]int{
			"input_tokens":   usage.inputTotal(),
			"output_tokens":  usage.OutputTokens,
			"context_tokens": usage.contextTotal(),
		}); err == nil {
			meta["usage"] = string(data)
		}
	}
	if contextWindow > 0 {
		meta["context_window"] = contextWindow
	}
	emit(engine.Event{Type: engine.EventDone, Meta: meta})
}

func modelUsageSummary(usages map[string]rawModelUsage) (*rawUsage, int) {
	var best rawModelUsage
	bestTotal := -1
	maxWindow := 0
	for _, usage := range usages {
		total := usage.InputTokens + usage.OutputTokens +
			usage.CacheReadInputTokens + usage.CacheCreationInputTokens
		if total > bestTotal {
			best = usage
			bestTotal = total
		}
		if usage.ContextWindow > maxWindow {
			maxWindow = usage.ContextWindow
		}
	}
	if bestTotal <= 0 {
		return nil, maxWindow
	}
	contextWindow := best.ContextWindow
	if contextWindow == 0 {
		contextWindow = maxWindow
	}
	return &rawUsage{
		InputTokens:              best.InputTokens,
		OutputTokens:             best.OutputTokens,
		CacheCreationInputTokens: best.CacheCreationInputTokens,
		CacheReadInputTokens:     best.CacheReadInputTokens,
	}, contextWindow
}

func decodeObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func toolSummary(tool string, args map[string]any) string {
	if args == nil {
		return tool
	}
	if description, ok := args["description"].(string); ok && strings.TrimSpace(description) != "" {
		return description
	}
	if command, ok := args["command"].(string); ok && strings.TrimSpace(command) != "" {
		return command
	}
	for _, key := range []string{"file_path", "path", "pattern", "url", "query"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			return tool + " " + value
		}
	}
	return tool
}

func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, block := range blocks {
			if block.Type == "text" {
				b.WriteString(block.Text)
			}
		}
		return strings.TrimSpace(b.String())
	}

	var object struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return ""
	}
	if object.Text != "" {
		return strings.TrimSpace(object.Text)
	}
	return strings.TrimSpace(object.Content)
}

func errorText(event rawEvent) string {
	if text := messageFromRaw(event.Error); text != "" {
		return text
	}
	if text := strings.TrimSpace(event.Result); text != "" {
		return text
	}
	if event.Message != nil {
		var b strings.Builder
		for _, content := range event.Message.Content {
			if content.Type == "text" {
				b.WriteString(content.Text)
			}
		}
		if text := strings.TrimSpace(b.String()); text != "" {
			return text
		}
	}
	return "claude reported an unknown error"
}

func messageFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var object struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return ""
	}
	if object.Message != "" {
		return strings.TrimSpace(object.Message)
	}
	return strings.TrimSpace(object.Error)
}
