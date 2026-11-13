package claude

import (
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestParseClaudeStream(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","subtype":"hook_started","session_id":"sess_1"}`,
		`{"type":"system","subtype":"init","session_id":"sess_1","model":"claude-sonnet-4-5"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":0,"output_tokens":0}}},"session_id":"sess_1"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"I'll"}},"session_id":"sess_1"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" check."}},"session_id":"sess_1"}`,
		`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"I'll check."},{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/tmp/a.txt"}}],"usage":{"input_tokens":10,"output_tokens":2}},"session_id":"sess_1"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"alpha"}]},"session_id":"sess_1"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Found it"}},"session_id":"sess_1"}`,
		`{"type":"assistant","message":{"id":"msg_2","content":[{"type":"text","text":"Found it"}],"usage":{"input_tokens":20,"output_tokens":3}},"session_id":"sess_1"}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"Found it","session_id":"sess_1","usage":{"input_tokens":20,"output_tokens":3,"cache_creation_input_tokens":4,"cache_read_input_tokens":5},"modelUsage":{"claude-sonnet-4-5":{"inputTokens":20,"outputTokens":3,"cacheReadInputTokens":4,"cacheCreationInputTokens":5,"contextWindow":200000}}}`,
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
	if got[0].SessionID != "sess_1" {
		t.Fatalf("session = %#v", got[0])
	}
	if got[4].Text != "Read /tmp/a.txt" || got[4].Meta["tool"] != "Read" || got[4].Meta["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool call = %#v", got[4])
	}
	if got[5].Text != "alpha" || got[5].Meta["status"] != "completed" {
		t.Fatalf("tool result = %#v", got[5])
	}
	if got[8].Meta["usage"] != `{"cached_input_tokens":9,"context_tokens":23,"input_tokens":20,"output_tokens":3}` {
		t.Fatalf("usage = %v", got[8].Meta["usage"])
	}
	if got[8].Meta["context_window"] != 200000 {
		t.Fatalf("context window = %v", got[8].Meta["context_window"])
	}
}

func TestParseClaudeToolError(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess_1"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"no such file"}],"is_error":true}]},"session_id":"sess_1"}`,
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
	if got[1].Type != engine.EventToolResult || got[1].Text != "no such file" || got[1].Meta["status"] != "error" {
		t.Fatalf("tool error = %#v", got[1])
	}
}

func TestParseClaudeResultError(t *testing.T) {
	input := `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"max turns reached","session_id":"sess_err"}`
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
	if got[1].Type != engine.EventError || got[1].Text != "max turns reached" {
		t.Fatalf("error = %#v", got[1])
	}
}

func TestParseClaudeUsageSplitsContextAndTurn(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"stream_event","event":{"type":"message_delta","usage":{"input_tokens":100,"output_tokens":1}},"session_id":"sess_1"}`,
		`{"type":"result","subtype":"success","result":"ok","session_id":"sess_1","usage":{"input_tokens":30,"output_tokens":2,"cache_creation_input_tokens":1,"cache_read_input_tokens":7},"modelUsage":{"m":{"inputTokens":999,"outputTokens":999,"contextWindow":1000000}}}`,
	}, "\n")

	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1<<20); err != nil {
		t.Fatal(err)
	}
	last := got[len(got)-1]
	if last.Type != engine.EventDone {
		t.Fatalf("last event = %#v", last)
	}
	if last.Meta["usage"] != `{"cached_input_tokens":8,"context_tokens":101,"input_tokens":30,"output_tokens":2}` {
		t.Fatalf("usage = %v", last.Meta["usage"])
	}
	if last.Meta["context_window"] != 1000000 {
		t.Fatalf("context window = %v", last.Meta["context_window"])
	}
}

func TestParseClaudeContextUsesLastStreamCall(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":16161,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}},"session_id":"sess_1"}`,
		`{"type":"stream_event","event":{"type":"message_delta","usage":{"input_tokens":16161,"output_tokens":60,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}},"session_id":"sess_1"}`,
		`{"type":"stream_event","event":{"type":"message_delta","usage":{"input_tokens":108,"output_tokens":18,"cache_read_input_tokens":16128,"cache_creation_input_tokens":0}},"session_id":"sess_1"}`,
		`{"type":"result","subtype":"success","result":"ok","session_id":"sess_1","usage":{"input_tokens":16269,"output_tokens":78,"cache_creation_input_tokens":0,"cache_read_input_tokens":16128},"modelUsage":{"m":{"inputTokens":16269,"outputTokens":78,"cacheReadInputTokens":16128,"cacheCreationInputTokens":0,"contextWindow":1000000}}}`,
	}, "\n")

	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1<<20); err != nil {
		t.Fatal(err)
	}
	last := got[len(got)-1]
	if last.Meta["usage"] != `{"cached_input_tokens":16128,"context_tokens":16254,"input_tokens":16269,"output_tokens":78}` {
		t.Fatalf("usage = %v", last.Meta["usage"])
	}
}
