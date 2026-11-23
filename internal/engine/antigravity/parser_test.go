package antigravity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func parseLines(t *testing.T, lines ...string) []engine.Event {
	t.Helper()
	var events []engine.Event
	err := Parse(strings.NewReader(strings.Join(lines, "\n")), func(event engine.Event) {
		events = append(events, event)
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestParseTextStreamAndUsage(t *testing.T) {
	events := parseLines(t,
		`{"event":"init","conversation_id":"c1","init":{"cwd":"/tmp/project","tools":["run_command"],"permission_mode":"request-review"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":0,"state":"DONE","step_type":"user_input"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":3,"state":"ACTIVE","step_type":"agent_response","text_delta":"Git rebase "}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":3,"state":"DONE","step_type":"agent_response","text_delta":"rewrites history.\n","usage":{"input_tokens":10302,"output_tokens":582,"thinking_tokens":551,"cache_read_tokens":8113,"total_tokens":10884}}}`,
		`{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"Git rebase rewrites history.\n","num_turns":1,"usage":{"input_tokens":10418,"output_tokens":589,"thinking_tokens":551,"cache_read_tokens":8113,"total_tokens":11007}}}`,
	)

	if len(events) == 0 || events[0].Type != engine.EventSessionStarted || events[0].SessionID != "c1" {
		t.Fatalf("events = %#v", events)
	}
	var text strings.Builder
	for _, event := range events {
		if event.Type == engine.EventTextDelta {
			text.WriteString(event.Text)
		}
		if event.Type == engine.EventMessage {
			t.Fatalf("unexpected duplicate final message: %#v", event)
		}
	}
	if got := text.String(); got != "Git rebase rewrites history.\n" {
		t.Fatalf("text = %q", got)
	}

	done := events[len(events)-1]
	if done.Type != engine.EventDone {
		t.Fatalf("last event = %#v", done)
	}
	var usage map[string]int
	raw, _ := done.Meta["usage"].(string)
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		t.Fatal(err)
	}
	if usage["context_tokens"] != 10884 || usage["input_tokens"] != 10418 ||
		usage["cached_input_tokens"] != 8113 || usage["output_tokens"] != 589 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestParseToolStep(t *testing.T) {
	events := parseLines(t,
		`{"event":"init","conversation_id":"c1","init":{"cwd":"/tmp/project"}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":4,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"echo hello"},"output":"hello\n"}}}`,
		`{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"done\n"}}`,
	)

	var call, result *engine.Event
	for i := range events {
		switch events[i].Type {
		case engine.EventToolCall:
			call = &events[i]
		case engine.EventToolResult:
			result = &events[i]
		}
	}
	if call == nil || call.Text != "echo hello" || call.Meta["tool"] != "run_command" {
		t.Fatalf("tool call = %#v", call)
	}
	if result == nil || result.Text != "hello\n" || result.Meta["status"] != "completed" {
		t.Fatalf("tool result = %#v", result)
	}
}

func TestParseResultFallsBackToMessage(t *testing.T) {
	events := parseLines(t,
		`{"event":"init","conversation_id":"c1","init":{}}`,
		`{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"apple\n"}}`,
	)

	if len(events) != 3 {
		t.Fatalf("events = %#v", events)
	}
	if events[1].Type != engine.EventMessage || events[1].Text != "apple" {
		t.Fatalf("message = %#v", events[1])
	}
	if events[2].Type != engine.EventDone {
		t.Fatalf("done = %#v", events[2])
	}
}

func TestParseErrorResult(t *testing.T) {
	events := parseLines(t,
		`{"event":"result","result":{"conversation_id":"c1","status":"ERROR","error":"invalid model selection"}}`,
	)
	if len(events) != 3 || events[0].Type != engine.EventSessionStarted {
		t.Fatalf("events = %#v", events)
	}
	if events[1].Type != engine.EventError || events[1].Text != "invalid model selection" {
		t.Fatalf("error = %#v", events[1])
	}
	if events[2].Type != engine.EventDone {
		t.Fatalf("done = %#v", events[2])
	}
}

func TestParseAuthError(t *testing.T) {
	events := parseLines(t,
		`{"event":"result","result":{"status":"ERROR","error":"You are not logged into Antigravity."}}`,
	)
	if len(events) != 2 || events[0].Type != engine.EventError || !strings.Contains(events[0].Text, "Run `agy` interactively") {
		t.Fatalf("events = %#v", events)
	}
	if events[1].Type != engine.EventDone {
		t.Fatalf("done = %#v", events[1])
	}
}

func TestParseIgnoresUnknownEvents(t *testing.T) {
	events := parseLines(t,
		`{"event":"future_event","payload":{"x":1}}`,
		`{"event":"result","result":{"status":"SUCCESS","response":"ok"}}`,
	)
	if len(events) != 2 || events[0].Type != engine.EventMessage || events[1].Type != engine.EventDone {
		t.Fatalf("events = %#v", events)
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	err := Parse(strings.NewReader("{not-json}"), func(engine.Event) {}, 0)
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
}

func TestParseDeduplicatesToolCallsAndErrorResults(t *testing.T) {
	events := parseLines(t,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"false"},"error":{"type":"ExecError","message":"exit status 1"}}}}`,
		`{"event":"step_update","step_update":{"conversation_id":"c1","step_index":2,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"false"},"error":{"type":"ExecError","message":"exit status 1"}}}}`,
		`{"event":"result","result":{"conversation_id":"c1","status":"SUCCESS","response":"done"}}`,
	)

	var calls, results int
	for _, ev := range events {
		switch ev.Type {
		case engine.EventToolCall:
			calls++
		case engine.EventToolResult:
			results++
			if ev.Meta["status"] != "error" || ev.Text != "exit status 1" {
				t.Fatalf("unexpected tool result = %#v", ev)
			}
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("calls=%d results=%d, want 1 and 1", calls, results)
	}
}

func TestParseRejectsOutputOverLimit(t *testing.T) {
	line := `{"event":"result","result":{"status":"SUCCESS","response":"hello"}}`
	err := Parse(strings.NewReader(line), func(engine.Event) {}, 10)
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("err = %v, want exceeded limit error", err)
	}
}
