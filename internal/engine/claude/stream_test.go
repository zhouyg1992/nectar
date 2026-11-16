package claude

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

func TestStreamAdapterSteersRunningTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec \"$NECTAR_TEST_BINARY\" -test.run=TestClaudeStreamFakeProcess -- \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NECTAR_TEST_BINARY", os.Args[0])
	t.Setenv("NECTAR_FAKE_CLAUDE_STREAM", "1")

	adapter := NewStreamAdapter(Config{Executable: script, WorkingDir: dir, Timeout: 5 * time.Second})
	events, err := adapter.Start(context.Background(), "do work", engine.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var got []engine.Event
	sawDone := false
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				if sawDone {
					goto done
				}
				t.Fatalf("events closed before completion: %#v", got)
			}
			got = append(got, event)
			if event.Type == engine.EventSessionStarted {
				if err := adapter.Steer(context.Background(), event.SessionID, "also test"); err != nil {
					t.Fatal(err)
				}
			}
			if event.Type == engine.EventDone {
				sawDone = true
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
	if err := adapter.Steer(context.Background(), "sess-claude", "too late"); err != engine.ErrNoActiveTurn {
		t.Fatalf("post-turn steer error = %v, want ErrNoActiveTurn", err)
	}
}

func TestClaudeStreamFakeProcess(t *testing.T) {
	if os.Getenv("NECTAR_FAKE_CLAUDE_STREAM") != "1" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	write := func(value any) {
		if err := encoder.Encode(value); err != nil {
			os.Exit(2)
		}
	}
	first := true
	for {
		var message struct {
			Type    string `json:"type"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := decoder.Decode(&message); err != nil {
			os.Exit(0)
		}
		if message.Type != "user" {
			continue
		}
		if first {
			first = false
			write(map[string]any{"type": "system", "subtype": "init", "session_id": "sess-claude"})
			continue
		}
		text := "steered: " + message.Message.Content
		write(map[string]any{
			"type":       "assistant",
			"session_id": "sess-claude",
			"message": map[string]any{
				"id":      "m1",
				"content": []any{map[string]any{"type": "text", "text": text}},
			},
		})
		write(map[string]any{
			"type":       "result",
			"subtype":    "success",
			"is_error":   false,
			"result":     text,
			"session_id": "sess-claude",
		})
	}
}
