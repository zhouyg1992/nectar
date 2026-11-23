package antigravity

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
	Event          string         `json:"event"`
	ConversationID string         `json:"conversation_id"`
	Init           *rawInit       `json:"init"`
	StepUpdate     *rawStepUpdate `json:"step_update"`
	Result         *rawResult     `json:"result"`
}

type rawInit struct {
	Cwd            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	Model          string `json:"model"`
	Agent          string `json:"agent"`
}

type rawStepUpdate struct {
	ConversationID  string       `json:"conversation_id"`
	StepIndex       int          `json:"step_index"`
	State           string       `json:"state"`
	StepType        string       `json:"step_type"`
	ToolName        string       `json:"tool_name"`
	TextDelta       string       `json:"text_delta"`
	DurationSeconds float64      `json:"duration_seconds"`
	Usage           *rawUsage    `json:"usage"`
	ToolInfo        *rawToolInfo `json:"tool_info"`
}

type rawToolInfo struct {
	Name       string          `json:"name"`
	Parameters map[string]any  `json:"parameters"`
	Output     json.RawMessage `json:"output"`
	Error      *rawToolError   `json:"error"`
}

type rawToolError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type rawResult struct {
	ConversationID   string          `json:"conversation_id"`
	Status           string          `json:"status"`
	Response         string          `json:"response"`
	Error            string          `json:"error"`
	DurationSeconds  float64         `json:"duration_seconds"`
	NumTurns         int             `json:"num_turns"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	Usage            *rawUsage       `json:"usage"`
}

type rawUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ThinkingTokens  int `json:"thinking_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
	TotalTokens     int `json:"total_tokens"`
}

func (u *rawUsage) empty() bool {
	if u == nil {
		return true
	}
	return u.InputTokens == 0 && u.OutputTokens == 0 && u.ThinkingTokens == 0 &&
		u.CacheReadTokens == 0 && u.TotalTokens == 0
}

func (u *rawUsage) contextTotal() int {
	if u == nil {
		return 0
	}
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.InputTokens + u.OutputTokens
}

// Parse translates Antigravity stream-json events into engine events.
func Parse(r io.Reader, emit func(engine.Event), maxOutputBytes int) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxEventLineBytes)

	total := 0
	conversationID := ""
	var deltaText strings.Builder
	var lastStepUsage *rawUsage
	seenTools := make(map[string]bool)
	seenResults := make(map[string]bool)
	sentDone := false

	emitSession := func(id string) {
		if id == "" || id == conversationID {
			return
		}
		conversationID = id
		emit(engine.Event{Type: engine.EventSessionStarted, SessionID: id})
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		total += len(line)
		if maxOutputBytes > 0 && total > maxOutputBytes {
			return fmt.Errorf("antigravity output exceeded %d bytes", maxOutputBytes)
		}

		var event rawEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("parse antigravity event: %w", err)
		}
		emitSession(event.ConversationID)

		switch event.Event {
		case "init":
			// Metadata only; the conversation id above is what Nectar persists.
		case "step_update":
			if event.StepUpdate == nil {
				continue
			}
			step := event.StepUpdate
			emitSession(step.ConversationID)
			if step.Usage != nil && !step.Usage.empty() {
				lastStepUsage = step.Usage
			}
			if step.StepType == "agent_response" && step.TextDelta != "" {
				deltaText.WriteString(step.TextDelta)
				emit(engine.Event{Type: engine.EventTextDelta, Text: step.TextDelta})
			}
			emitToolStep(step, seenTools, seenResults, emit)
		case "result":
			if event.Result == nil {
				continue
			}
			result := event.Result
			emitSession(result.ConversationID)
			if err := emitResult(result, deltaText.String(), emit); err != nil {
				return err
			}
			if meta := usageMeta(lastStepUsage, result.Usage); meta != nil {
				emit(engine.Event{Type: engine.EventDone, Meta: meta})
			} else {
				emit(engine.Event{Type: engine.EventDone})
			}
			sentDone = true
		default:
			// Ignore unknown event names so a newer CLI can add events without
			// breaking Nectar. Malformed JSON still fails above.
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read antigravity output: %w", err)
	}
	if !sentDone {
		emit(engine.Event{Type: engine.EventDone})
	}
	return nil
}

func emitResult(result *rawResult, deltaText string, emit func(engine.Event)) error {
	status := strings.ToUpper(strings.TrimSpace(result.Status))
	errorText := strings.TrimSpace(result.Error)
	if (status != "" && status != "SUCCESS") || (status == "" && errorText != "") {
		text := errorText
		if text == "" {
			text = strings.TrimSpace(result.Response)
		}
		if text == "" {
			text = "antigravity reported " + status
		}
		emit(engine.Event{Type: engine.EventError, Text: authErrorMessage(text)})
		return nil
	}

	response := strings.TrimSpace(result.Response)
	if response != "" && response != strings.TrimSpace(deltaText) {
		emit(engine.Event{Type: engine.EventMessage, Text: response})
	}
	return nil
}

func emitToolStep(step *rawStepUpdate, seenTools, seenResults map[string]bool, emit func(engine.Event)) {
	if step.StepType != "tool" || step.ToolInfo == nil {
		return
	}
	info := step.ToolInfo
	name := strings.TrimSpace(info.Name)
	if name == "" {
		name = strings.TrimSpace(step.ToolName)
	}
	if name == "" {
		return
	}

	key := fmt.Sprintf("%d|%s", step.StepIndex, name)
	if !seenTools[key] {
		seenTools[key] = true
		emit(engine.Event{
			Type: engine.EventToolCall,
			Text: toolSummary(name, info.Parameters),
			Meta: map[string]any{
				"tool":   name,
				"status": "started",
				"input":  info.Parameters,
			},
		})
	}

	status := "completed"
	text := rawText(info.Output)
	if info.Error != nil {
		status = "error"
		text = strings.TrimSpace(info.Error.Message)
		if text == "" {
			text = strings.TrimSpace(info.Error.Type)
		}
	}
	if text == "" {
		if status != "error" {
			return
		}
		text = name + " failed"
	}
	if step.State != "DONE" && status != "error" {
		return
	}
	if seenResults[key] {
		return
	}
	seenResults[key] = true
	emit(engine.Event{
		Type: engine.EventToolResult,
		Text: text,
		Meta: map[string]any{
			"tool":   name,
			"status": status,
		},
	})
}

func toolSummary(name string, params map[string]any) string {
	for _, key := range []string{"CommandLine", "command", "path", "file_path", "filePath", "file", "query", "pattern"} {
		value, ok := params[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		if key == "CommandLine" || key == "command" {
			return value
		}
		return name + " " + value
	}
	return name
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		if encoded, err := json.Marshal(value); err == nil {
			return string(encoded)
		}
	}
	return string(raw)
}

func usageMeta(stepUsage, resultUsage *rawUsage) map[string]any {
	data := map[string]int{}
	if stepUsage != nil && !stepUsage.empty() {
		data["context_tokens"] = stepUsage.contextTotal()
	}
	if resultUsage != nil && !resultUsage.empty() {
		data["input_tokens"] = resultUsage.InputTokens
		data["cached_input_tokens"] = resultUsage.CacheReadTokens
		data["output_tokens"] = resultUsage.OutputTokens
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
