package pi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestRPCAdapterSteersRunningTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "pi")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec \"$NECTAR_TEST_BINARY\" -test.run=TestPiRPCFakeProcess -- \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NECTAR_TEST_BINARY", os.Args[0])
	t.Setenv("NECTAR_FAKE_PI_RPC", "1")

	adapter := NewRPCAdapter(Config{Executable: script, WorkingDir: dir, Timeout: 5 * time.Second})
	events, err := adapter.Start(context.Background(), "do work", engine.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var got []engine.Event
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("events closed before steering: %#v", got)
			}
			got = append(got, event)
			if event.Type == engine.EventSessionStarted {
				if err := adapter.Steer(context.Background(), event.SessionID, "also test"); err != nil {
					t.Fatal(err)
				}
			}
			if event.Type == engine.EventDone {
				goto done
			}
		case <-deadline:
			t.Fatalf("timed out; events = %#v", got)
		}
	}

done:
	var message string
	for _, event := range got {
		if event.Type == engine.EventMessage {
			message = event.Text
		}
	}
	if message != "steered: also test" {
		t.Fatalf("message = %q, events = %#v", message, got)
	}
	if err := adapter.Steer(context.Background(), "sess-pi", "too late"); err != engine.ErrNoActiveTurn {
		t.Fatalf("post-turn steer error = %v, want ErrNoActiveTurn", err)
	}
}

func TestPiRPCFakeProcess(t *testing.T) {
	if os.Getenv("NECTAR_FAKE_PI_RPC") != "1" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	write := func(value any) {
		if err := encoder.Encode(value); err != nil {
			os.Exit(2)
		}
	}
	for {
		var command struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if err := decoder.Decode(&command); err != nil {
			os.Exit(0)
		}
		switch command.Type {
		case "get_state":
			write(map[string]any{"type": "response", "command": "get_state", "success": true, "data": map[string]any{"sessionId": "sess-pi", "isStreaming": false}})
		case "prompt":
			write(map[string]any{"type": "message_start", "message": map[string]any{"role": "system", "content": "system prompt"}})
			write(map[string]any{"type": "message_start", "message": map[string]any{"role": "assistant"}})
			write(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "waiting"}})
		case "steer":
			write(map[string]any{
				"type": "message_end",
				"message": map[string]any{
					"role":    "assistant",
					"content": []any{map[string]any{"type": "text", "text": "steered: " + command.Message}},
				},
			})
			write(map[string]any{"type": "agent_end"})
			write(map[string]any{"type": "agent_settled", "messages": []any{}})
		}
	}
}
