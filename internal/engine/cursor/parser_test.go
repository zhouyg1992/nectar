package cursor

import (
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestParseCursorStream(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess_1","model":"Auto","permissionMode":"default"}`,
		`{"type":"thinking","subtype":"delta","text":"thinking","session_id":"sess_1","timestamp_ms":1}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll"}]},"session_id":"sess_1","timestamp_ms":2}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":" check."}]},"session_id":"sess_1","timestamp_ms":3}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll check."}]},"session_id":"sess_1","timestamp_ms":4}`,
		`{"type":"tool_call","subtype":"started","tool_call":{"shellToolCall":{"args":{"command":"cat input.txt"},"description":"Print contents of input.txt"}},"session_id":"sess_1"}`,
		`{"type":"tool_call","subtype":"completed","tool_call":{"shellToolCall":{"args":{"command":"cat input.txt"},"result":{"success":{"stdout":"hello\n","exitCode":0}},"description":"Print contents of input.txt"}},"session_id":"sess_1"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Found"}]},"session_id":"sess_1","timestamp_ms":5}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":" a.txt"}]},"session_id":"sess_1","timestamp_ms":6}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Found a.txt"}]},"session_id":"sess_1","timestamp_ms":7}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Found a.txt"}]},"session_id":"sess_1"}`,
		`{"type":"usage","subtype":"model_call","usage":{"inputTokens":100,"outputTokens":5,"cacheReadTokens":50,"cacheWriteTokens":0,"reasoningTokens":2},"session_id":"sess_1"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Found a.txt","session_id":"sess_1","usage":{"inputTokens":10,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":0,"reasoningTokens":1}}`,
	}, "\n")

	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1<<20); err != nil {
		t.Fatal(err)
	}

	if len(got) != 10 {
		t.Fatalf("events = %d, want 10: %#v", len(got), got)
	}
	if got[0].Type != engine.EventSessionStarted || got[0].SessionID != "sess_1" {
		t.Fatalf("session = %#v", got[0])
	}
	if got[1].Type != engine.EventTextDelta || got[1].Text != "I'll" {
		t.Fatalf("delta 1 = %#v", got[1])
	}
	if got[2].Type != engine.EventTextDelta || got[2].Text != " check." {
		t.Fatalf("delta 2 = %#v", got[2])
	}
	if got[3].Type != engine.EventMessage || got[3].Text != "I'll check." {
		t.Fatalf("progress message = %#v", got[3])
	}
	if got[4].Type != engine.EventToolCall || got[4].Text != "Print contents of input.txt" || got[4].Meta["tool"] != "shell" {
		t.Fatalf("tool call = %#v", got[4])
	}
	if got[5].Type != engine.EventToolResult || got[5].Text != "hello\n" || got[5].Meta["status"] != "completed" {
		t.Fatalf("tool result = %#v", got[5])
	}
	if got[6].Type != engine.EventTextDelta || got[6].Text != "Found" {
		t.Fatalf("delta 3 = %#v", got[6])
	}
	if got[7].Type != engine.EventTextDelta || got[7].Text != " a.txt" {
		t.Fatalf("delta 4 = %#v", got[7])
	}
	if got[8].Type != engine.EventMessage || got[8].Text != "Found a.txt" {
		t.Fatalf("final message = %#v", got[8])
	}
	if got[9].Type != engine.EventDone {
		t.Fatalf("done = %#v", got[9])
	}
	if got[9].Meta["usage"] != `{"cached_input_tokens":3,"context_tokens":155,"input_tokens":10,"output_tokens":2}` {
		t.Fatalf("usage = %v", got[9].Meta["usage"])
	}
}

func TestParseCursorReadTool(t *testing.T) {
	input := `{"type":"tool_call","subtype":"completed","tool_call":{"readToolCall":{"args":{"path":"/tmp/a.txt"},"result":{"success":{"content":"alpha","isEmpty":false}}}},"session_id":"sess_1"}`
	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events = %#v", got)
	}
	if got[1].Type != engine.EventToolResult || got[1].Text != "alpha" || got[1].Meta["tool"] != "read" {
		t.Fatalf("tool result = %#v", got[1])
	}
}

func TestParseCursorToolErrorIsRecoverable(t *testing.T) {
	input := `{"type":"tool_call","subtype":"completed","tool_call":{"readToolCall":{"args":{"path":"/missing"},"result":{"error":{"message":"no such file"}}}},"session_id":"sess_1"}`
	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events = %#v", got)
	}
	if got[1].Type != engine.EventToolResult || got[1].Text != "no such file" || got[1].Meta["status"] != "error" {
		t.Fatalf("tool error = %#v", got[1])
	}
}

func TestParseCursorResultError(t *testing.T) {
	input := `{"type":"result","subtype":"error","is_error":true,"result":"model unavailable","session_id":"sess_err"}`
	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events = %#v", got)
	}
	if got[0].Type != engine.EventSessionStarted || got[0].SessionID != "sess_err" {
		t.Fatalf("session = %#v", got[0])
	}
	if got[1].Type != engine.EventError || got[1].Text != "model unavailable" {
		t.Fatalf("error = %#v", got[1])
	}
}

func TestParseCursorAuthErrorIsActionable(t *testing.T) {
	input := `{"type":"result","subtype":"error","is_error":true,"result":"Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable.","session_id":"sess_err"}`
	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events = %#v", got)
	}
	if got[1].Type != engine.EventError {
		t.Fatalf("error = %#v", got[1])
	}
	if !strings.Contains(got[1].Text, "agent login") || !strings.Contains(got[1].Text, "CURSOR_API_KEY") {
		t.Fatalf("error text = %q", got[1].Text)
	}
}

func TestParseCursorDeduplicatesAssistantEcho(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"alpha"}]},"session_id":"sess_1"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"alpha"}]},"session_id":"sess_1"}`,
	}, "\n")
	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events = %#v", got)
	}
	if got[1].Type != engine.EventMessage || got[1].Text != "alpha" {
		t.Fatalf("message = %#v", got[1])
	}
}

func TestParseCursorOutputLimit(t *testing.T) {
	input := strings.Repeat(`{"type":"system","subtype":"init","session_id":"sess_1"}`+"\n", 10)
	if err := Parse(strings.NewReader(input), func(engine.Event) {}, 20); err == nil {
		t.Fatal("expected output limit error")
	}
}
