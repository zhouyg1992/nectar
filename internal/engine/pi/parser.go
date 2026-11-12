package pi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/zhouyg1992/nectar/internal/engine"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
)

const (
	maxEventLineBytes   = 4 << 20
	maxToolResultLength = 8 << 10
)

type rawEvent struct {
	Type                  string             `json:"type"`
	ID                    string             `json:"id"`
	Cwd                   string             `json:"cwd"`
	Message               *rawMessage        `json:"message"`
	Messages              []rawMessage       `json:"messages"`
	AssistantMessageEvent *rawAssistantEvent `json:"assistantMessageEvent"`
	Usage                 *rawUsage          `json:"usage"`
	ToolCallID            string             `json:"toolCallId"`
	ToolName              string             `json:"toolName"`
	Args                  json.RawMessage    `json:"args"`
	Result                json.RawMessage    `json:"result"`
	IsError               bool               `json:"isError"`
	ErrorMessage          string             `json:"errorMessage"`
}

type rawAssistantEvent struct {
	Type     string `json:"type"`
	Delta    string `json:"delta"`
	ID       string `json:"id"`
	ToolName string `json:"toolName"`
}

type rawMessage struct {
	Role         string       `json:"role"`
	Content      []rawContent `json:"content"`
	Usage        *rawUsage    `json:"usage"`
	Model        string       `json:"model"`
	StopReason   string       `json:"stopReason"`
	ErrorMessage string       `json:"errorMessage"`
}

type rawContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type rawUsage struct {
	Input       int `json:"input"`
	Output      int `json:"output"`
	CacheRead   int `json:"cacheRead"`
	CacheWrite  int `json:"cacheWrite"`
	TotalTokens int `json:"totalTokens"`
}

func (u *rawUsage) inputTotal() int {
	return u.Input + u.CacheRead + u.CacheWrite
}

func (u *rawUsage) contextTotal() int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.inputTotal() + u.Output
}

func (u *rawUsage) empty() bool {
	return u.contextTotal() == 0
}

// Parse translates Pi's JSON event stream into engine events.
func Parse(r io.Reader, emit func(engine.Event), maxOutputBytes int) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxEventLineBytes)

	total := 0
	sessionID := ""
	var stream strings.Builder
	var lastUsage *rawUsage
	lastMessage := ""
	lastError := ""
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
	emitError := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" || text == lastError {
			return
		}
		lastError = text
		emit(engine.Event{Type: engine.EventError, Text: text})
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		total += len(line)
		if maxOutputBytes > 0 && total > maxOutputBytes {
			return fmt.Errorf("pi output exceeded %d bytes", maxOutputBytes)
		}

		var event rawEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("parse pi event: %w", err)
		}

		if event.Type == "session" && event.ID != "" && event.ID != sessionID {
			sessionID = event.ID
			emit(engine.Event{Type: engine.EventSessionStarted, SessionID: sessionID})
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil && event.Message.Role == "assistant" {
				stream.Reset()
			}
		case "message_update":
			if event.Usage != nil && !event.Usage.empty() {
				lastUsage = event.Usage
			}
			if event.AssistantMessageEvent == nil {
				continue
			}
			if event.AssistantMessageEvent.Type == "text_delta" && event.AssistantMessageEvent.Delta != "" {
				stream.WriteString(event.AssistantMessageEvent.Delta)
				emit(engine.Event{Type: engine.EventTextDelta, Text: event.AssistantMessageEvent.Delta})
			}
		case "message_end":
			if event.Message == nil {
				continue
			}
			if event.Message.Role != "assistant" {
				continue
			}
			if event.Message.Usage != nil && !event.Message.Usage.empty() {
				lastUsage = event.Message.Usage
			}
			if text := messageText(event.Message); text != "" {
				emitMessage(text)
			}
			if isMessageError(event.Message) {
				emitError(messageErrorText(event.Message))
			}
			stream.Reset()
		case "tool_execution_start":
			emitToolCall(event, emit)
		case "tool_execution_end":
			emitToolResult(event, emit)
		case "compaction_end":
			emitError(event.ErrorMessage)
		case "agent_end":
			if usage := agentEndUsage(event); usage != nil {
				lastUsage = usage
			}
			if last := lastAssistantMessage(event.Messages); last != nil && isMessageError(last) {
				emitError(messageErrorText(last))
			}
			emit(engine.Event{Type: engine.EventDone, Meta: usageMeta(lastUsage)})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read pi output: %w", err)
	}
	return nil
}

func messageText(message *rawMessage) string {
	if message == nil {
		return ""
	}
	var text strings.Builder
	for _, content := range message.Content {
		if content.Type == "text" {
			text.WriteString(content.Text)
		}
	}
	return strings.TrimSpace(text.String())
}

func isMessageError(message *rawMessage) bool {
	if message == nil {
		return false
	}
	if strings.TrimSpace(message.ErrorMessage) != "" {
		return true
	}
	return message.StopReason == "error" || message.StopReason == "aborted"
}

func messageErrorText(message *rawMessage) string {
	if message == nil {
		return "pi request failed"
	}
	if text := strings.TrimSpace(message.ErrorMessage); text != "" {
		return text
	}
	return "pi request " + message.StopReason
}

func lastAssistantMessage(messages []rawMessage) *rawMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return &messages[i]
		}
	}
	return nil
}

func agentEndUsage(event rawEvent) *rawUsage {
	if last := lastAssistantMessage(event.Messages); last != nil && last.Usage != nil && !last.Usage.empty() {
		return last.Usage
	}
	return nil
}

func emitToolCall(event rawEvent, emit func(engine.Event)) {
	tool := strings.TrimSpace(event.ToolName)
	if tool == "" {
		tool = "tool"
	}
	args := decodeObject(event.Args)
	emit(engine.Event{
		Type: engine.EventToolCall,
		Text: toolSummary(tool, args),
		Meta: map[string]any{
			"tool":        tool,
			"tool_use_id": event.ToolCallID,
			"status":      "started",
			"input":       args,
		},
	})
}

func emitToolResult(event rawEvent, emit func(engine.Event)) {
	text := toolResultText(event.Result)
	status := "completed"
	if event.IsError {
		status = "error"
	}
	if text == "" && status != "error" {
		return
	}
	if text == "" {
		text = "tool failed"
	}
	emit(engine.Event{
		Type: engine.EventToolResult,
		Text: execguard.Truncate(text, maxToolResultLength),
		Meta: map[string]any{
			"tool":        strings.TrimSpace(event.ToolName),
			"tool_use_id": event.ToolCallID,
			"status":      status,
		},
	})
}

func usageMeta(usage *rawUsage) map[string]any {
	if usage == nil || usage.empty() {
		return nil
	}
	data, err := json.Marshal(map[string]int{
		"input_tokens":   usage.inputTotal(),
		"output_tokens":  usage.Output,
		"context_tokens": usage.contextTotal(),
	})
	if err != nil {
		return nil
	}
	return map[string]any{"usage": string(data)}
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
	for _, key := range []string{"path", "file_path", "pattern", "url", "query"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			return tool + " " + value
		}
	}
	return tool
}

func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}

	var blocks []rawContent
	if err := json.Unmarshal(raw, &blocks); err == nil {
		if joined := contentText(blocks); joined != "" {
			return joined
		}
	}

	var object struct {
		Output  string          `json:"output"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
		Stdout  string          `json:"stdout"`
		Stderr  string          `json:"stderr"`
		Error   string          `json:"error"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return ""
	}
	if len(object.Content) > 0 {
		var content string
		if err := json.Unmarshal(object.Content, &content); err == nil && strings.TrimSpace(content) != "" {
			return strings.TrimSpace(content)
		}
		var blocks []rawContent
		if err := json.Unmarshal(object.Content, &blocks); err == nil {
			if joined := contentText(blocks); joined != "" {
				return joined
			}
		}
	}
	for _, value := range []string{object.Output, object.Text, object.Stdout, object.Stderr, object.Error, object.Message} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func contentText(blocks []rawContent) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(text.String())
}
