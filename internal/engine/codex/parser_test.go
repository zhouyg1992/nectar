package codex

import (
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestParseCodexEvents(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"id":"i1","type":"command_execution","command":"ls"}}`,
		`{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"ls","aggregated_output":"a.go","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"working","phase":"commentary"}}`,
		`{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":"done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10}}`,
	}, "\n")

	var got []engine.Event
	err := Parse(strings.NewReader(input), func(ev engine.Event) {
		got = append(got, ev)
	}, 1<<20)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(got) != 6 {
		t.Fatalf("got %d events, want 6: %#v", len(got), got)
	}
	if got[0].Type != engine.EventSessionStarted || got[0].SessionID != "thread-1" {
		t.Fatalf("first event = %#v", got[0])
	}
	if got[1].Type != engine.EventToolCall || got[1].Text != "ls" {
		t.Fatalf("tool call = %#v", got[1])
	}
	if got[2].Type != engine.EventToolResult || got[2].Text != "a.go" {
		t.Fatalf("tool result = %#v", got[2])
	}
	if got[3].Type != engine.EventMessage || got[3].Text != "working" || got[3].Meta["phase"] != "commentary" {
		t.Fatalf("commentary = %#v", got[3])
	}
	if got[4].Type != engine.EventMessage || got[4].Text != "done" {
		t.Fatalf("message = %#v", got[4])
	}
	if got[5].Type != engine.EventDone {
		t.Fatalf("last event = %#v", got[5])
	}
}

func TestParseTurnFailed(t *testing.T) {
	input := `{"type":"turn.failed","error":{"message":"quota exceeded"}}`
	var got []engine.Event
	if err := Parse(strings.NewReader(input), func(ev engine.Event) { got = append(got, ev) }, 1024); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != engine.EventError || got[0].Text != "quota exceeded" {
		t.Fatalf("events = %#v", got)
	}
}

func TestParseOutputLimit(t *testing.T) {
	input := strings.Repeat(`{"type":"turn.started"}`+"\n", 10)
	err := Parse(strings.NewReader(input), func(engine.Event) {}, 20)
	if err == nil {
		t.Fatal("expected output limit error")
	}
}

func TestBuildArgs(t *testing.T) {
	adapter := NewAdapter(Config{
		Executable: "codex",
		Model:      "gpt-test",
		Effort:     "high",
		Sandbox:    "workspace-write",
	})

	start := strings.Join(adapter.buildArgs("", engine.EngineOptions{}), " ")
	for _, want := range []string{"exec --json", `approval_policy="never"`, "--sandbox workspace-write", "--model gpt-test", `model_reasoning_effort="high"`, " -"} {
		if !strings.Contains(start, want) {
			t.Fatalf("start args %q missing %q", start, want)
		}
	}

	resume := strings.Join(adapter.buildArgs("thread-1", engine.EngineOptions{}), " ")
	if !strings.Contains(resume, "exec resume thread-1") {
		t.Fatalf("resume args = %q", resume)
	}
	if strings.Contains(resume, "--sandbox") {
		t.Fatalf("resume should not pass --sandbox: %q", resume)
	}
}

func TestBuildArgsIncludesImages(t *testing.T) {
	adapter := NewAdapter(Config{Executable: "codex"})
	opts := engine.EngineOptions{Attachments: []attachment.Local{
		{Kind: attachment.KindImage, Path: "/tmp/image.png"},
		{Kind: attachment.KindFile, Path: "/tmp/report.pdf"},
	}}

	start := strings.Join(adapter.buildArgs("", opts), " ")
	if !strings.Contains(start, "--image /tmp/image.png") {
		t.Fatalf("start args = %q", start)
	}
	if strings.Contains(start, "--image /tmp/report.pdf") {
		t.Fatalf("file should not use --image: %q", start)
	}

	resume := strings.Join(adapter.buildArgs("thread-1", opts), " ")
	if !strings.Contains(resume, "--image /tmp/image.png") {
		t.Fatalf("resume args = %q", resume)
	}
}

func TestWithoutEnv(t *testing.T) {
	got := withoutEnv([]string{"A=1", "FEISHU_APP_SECRET=x", "B=2"}, "FEISHU_APP_SECRET")
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Fatalf("withoutEnv() = %#v", got)
	}
}
