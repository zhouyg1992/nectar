package pi

import (
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestParsePiEvents(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess_pi","timestamp":"2026-11-12T12:00:00Z","cwd":"/tmp"}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[],"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0},"stopReason":"stop","timestamp":1}}`,
		`{"type":"message_update","usage":{"input":10,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":11},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Hel"}}`,
		`{"type":"message_update","usage":{"input":10,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":12},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"lo"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}],"usage":{"input":10,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":12},"stopReason":"stop","timestamp":2}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"ls"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"output":"a.go\n"},"isError":false}`,
		`{"type":"message_start","message":{"role":"assistant","content":[],"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0},"stopReason":"stop","timestamp":3}}`,
		`{"type":"message_update","usage":{"input":20,"output":3,"cacheRead":5,"cacheWrite":1,"totalTokens":29},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"Found"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Found"}],"usage":{"input":20,"output":3,"cacheRead":5,"cacheWrite":1,"totalTokens":29},"stopReason":"stop","timestamp":4}}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"Found"}],"usage":{"input":20,"output":3,"cacheRead":5,"cacheWrite":1,"totalTokens":29},"stopReason":"stop","timestamp":4}]}`,
	}, "\n")

	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1<<20); err != nil {
		t.Fatal(err)
	}

	wantTypes := []engine.EventType{
		engine.EventSessionStarted,
		engine.EventTextDelta,
		engine.EventTextDelta,
		engine.EventMessage,
		engine.EventToolCall,
		engine.EventToolResult,
		engine.EventTextDelta,
		engine.EventMessage,
		engine.EventDone,
	}
	if len(got) != len(wantTypes) {
		t.Fatalf("events = %#v", got)
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Fatalf("event %d = %#v, want %s", i, got[i], want)
		}
	}
	if got[0].SessionID != "sess_pi" {
		t.Fatalf("session = %#v", got[0])
	}
	if got[3].Text != "Hello" {
		t.Fatalf("message = %#v", got[3])
	}
	if got[4].Text != "ls" || got[4].Meta["tool"] != "bash" || got[4].Meta["tool_use_id"] != "call_1" {
		t.Fatalf("tool call = %#v", got[4])
	}
	if got[5].Text != "a.go" || got[5].Meta["status"] != "completed" {
		t.Fatalf("tool result = %#v", got[5])
	}
	if got[8].Meta["usage"] != `{"cached_input_tokens":6,"context_tokens":29,"input_tokens":30,"output_tokens":5}` {
		t.Fatalf("usage = %v", got[8].Meta["usage"])
	}
}

func TestParsePiMessageError(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"session","id":"sess_err"}`,
		`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"model unavailable"}}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[],"stopReason":"error","errorMessage":"model unavailable"}]}`,
	}, "\n")

	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("events = %#v", got)
	}
	if got[1].Type != engine.EventError || got[1].Text != "model unavailable" {
		t.Fatalf("error = %#v", got[1])
	}
	if got[2].Type != engine.EventDone {
		t.Fatalf("done = %#v", got[2])
	}
}

func TestParsePiToolErrorAndContentBlocks(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"tool_execution_end","toolCallId":"call_2","toolName":"read","result":{"content":[{"type":"text","text":"no such file"}]},"isError":true}`,
	}, "\n")

	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("events = %#v", got)
	}
	if got[0].Type != engine.EventToolResult || got[0].Text != "no such file" || got[0].Meta["status"] != "error" {
		t.Fatalf("tool error = %#v", got[0])
	}
}

func TestParsePiOutputLimit(t *testing.T) {
	input := strings.Repeat(`{"type":"turn_start"}`+"\n", 10)
	err := Parse(strings.NewReader(input), func(engine.Event) {}, 20)
	if err == nil {
		t.Fatal("expected output limit error")
	}
}
