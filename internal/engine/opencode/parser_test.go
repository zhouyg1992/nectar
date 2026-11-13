package opencode

import (
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestParseOpenCodeEvents(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"step_start","sessionID":"ses_1","part":{"type":"step-start"}}`,
		`{"type":"text","sessionID":"ses_1","part":{"type":"text","text":"I'll read both files."}}`,
		`{"type":"tool_use","sessionID":"ses_1","part":{"type":"tool","tool":"read","state":{"status":"completed","input":{"path":"/tmp/a.txt"},"output":"alpha"}}}`,
		`{"type":"step_finish","sessionID":"ses_1","part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":10,"output":2}}}`,
		`{"type":"text","sessionID":"ses_1","part":{"type":"text","text":"Final answer"}}`,
	}, "\n")

	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1<<20); err != nil {
		t.Fatal(err)
	}

	if len(got) != 5 {
		t.Fatalf("events = %d, want 5: %#v", len(got), got)
	}
	if got[0].Type != engine.EventSessionStarted || got[0].SessionID != "ses_1" {
		t.Fatalf("session = %#v", got[0])
	}
	if got[1].Type != engine.EventMessage || got[1].Text != "I'll read both files." {
		t.Fatalf("progress = %#v", got[1])
	}
	if got[2].Type != engine.EventToolCall || !strings.Contains(got[2].Text, "/tmp/a.txt") {
		t.Fatalf("tool call = %#v", got[2])
	}
	if got[3].Type != engine.EventToolResult || got[3].Text != "alpha" {
		t.Fatalf("tool result = %#v", got[3])
	}
	if got[4].Type != engine.EventMessage || got[4].Text != "Final answer" {
		t.Fatalf("final = %#v", got[4])
	}
}

func TestParseOpenCodeError(t *testing.T) {
	input := `{"type":"error","sessionID":"ses_1","error":{"message":"auth failed"}}`
	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Type != engine.EventError || got[1].Text != "auth failed" {
		t.Fatalf("events = %#v", got)
	}
}

func TestParseOpenCodeTurnUsage(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"step_finish","sessionID":"ses_1","part":{"type":"step-finish","tokens":{"input":10,"output":2,"reasoning":1,"cache":{"read":5,"write":0}}}}`,
		`{"type":"step_finish","sessionID":"ses_1","part":{"type":"step-finish","tokens":{"input":4,"output":1,"reasoning":3,"cache":{"read":0,"write":1}}}}`,
	}, "\n")

	usage, err := ParseWithUsage(strings.NewReader(input), func(engine.Event) {}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if usage == nil || usage.Turn.empty() {
		t.Fatalf("usage = %#v", usage)
	}
	if usage.Turn.Input != 14 || usage.Turn.Output != 3 || usage.Turn.Reasoning != 4 ||
		usage.Turn.Cache.Read != 5 || usage.Turn.Cache.Write != 1 {
		t.Fatalf("turn usage = %#v", usage.Turn)
	}
	if usage.Last.Input != 4 || usage.Last.Output != 1 || usage.Last.Reasoning != 3 {
		t.Fatalf("last usage = %#v", usage.Last)
	}
}

func TestParseOpenCodeToolErrorIsRecoverable(t *testing.T) {
	input := `{"type":"tool_use","sessionID":"ses_1","part":{"type":"tool","tool":"read","state":{"status":"error","input":{"path":"/missing"},"output":"no such file"}}}`
	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("events = %#v", got)
	}
	if got[2].Type != engine.EventToolResult || got[2].Text != "no such file" || got[2].Meta["status"] != "error" {
		t.Fatalf("tool error = %#v", got[2])
	}
}

func TestParseOpenCodeOutputLimit(t *testing.T) {
	input := strings.Repeat(`{"type":"step_start","sessionID":"ses_1"}`+"\n", 10)
	if err := Parse(strings.NewReader(input), func(engine.Event) {}, 20); err == nil {
		t.Fatal("expected output limit error")
	}
}
