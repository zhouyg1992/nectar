package oncall

import (
	"context"
	"fmt"
	"strings"

	"github.com/zhouyg1992/nectar/internal/engine"
)

// AgentRunner runs one isolated engine turn and returns its final text.
type AgentRunner interface {
	Run(ctx context.Context, prompt string, opts engine.EngineOptions) (RunResult, error)
}

type EngineRunner struct {
	Engine engine.EngineAdapter
}

func (r *EngineRunner) Run(ctx context.Context, prompt string, opts engine.EngineOptions) (RunResult, error) {
	events, err := r.Engine.Start(ctx, prompt, opts)
	if err != nil {
		return RunResult{}, fmt.Errorf("start on-call agent: %w", err)
	}
	var result RunResult
	var stream strings.Builder
	var finalMessage, lastMessage, explicitFinal string
	var errorsSeen []string
	for ev := range events {
		switch ev.Type {
		case engine.EventSessionStarted:
			result.SessionID = ev.SessionID
		case engine.EventMessage:
			lastMessage = ev.Text
			if phase, _ := ev.Meta["phase"].(string); phase == "final_answer" {
				explicitFinal = ev.Text
			}
			if phase, _ := ev.Meta["phase"].(string); phase != "commentary" {
				finalMessage = ev.Text
			}
		case engine.EventTextDelta:
			stream.WriteString(ev.Text)
			lastMessage = stream.String()
		case engine.EventToolCall:
			result.ToolCalls++
		case engine.EventError:
			if text := strings.TrimSpace(ev.Text); text != "" {
				errorsSeen = append(errorsSeen, text)
			}
		}
	}
	if ctx.Err() != nil {
		errorsSeen = append(errorsSeen, ctx.Err().Error())
	}
	result.Output = firstNonEmpty(explicitFinal, finalMessage, stream.String(), lastMessage)
	result.Output = strings.TrimSpace(result.Output)
	if result.Output == "" {
		result.Output = "(agent returned no text output)"
	}
	if len(errorsSeen) > 0 {
		return result, fmt.Errorf("%s", strings.Join(errorsSeen, "\n"))
	}
	return result, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
