package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestAppServerAdapterSteersRunningTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	body := "#!/bin/sh\nexec \"$NECTAR_TEST_BINARY\" -test.run=TestAppServerFakeProcess -- \"$@\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NECTAR_TEST_BINARY", os.Args[0])
	t.Setenv("NECTAR_FAKE_APPSERVER", "1")

	adapter := NewAppServerAdapter(Config{Executable: script, WorkingDir: dir, Timeout: 5 * time.Second})
	events, err := adapter.Start(context.Background(), "do the task", engine.EngineOptions{})
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
				if event.SessionID != "thread-1" {
					t.Fatalf("session = %q", event.SessionID)
				}
				if err := adapter.Steer(context.Background(), event.SessionID, "also check tests"); err != nil {
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
	if message != "steered: also check tests" {
		t.Fatalf("message = %q, events = %#v", message, got)
	}
	if err := adapter.Steer(context.Background(), "thread-1", "too late"); err != engine.ErrNoActiveTurn {
		t.Fatalf("post-turn steer error = %v, want ErrNoActiveTurn", err)
	}
}

// TestAppServerFakeProcess is a tiny app-server peer used by the parent test.
// It deliberately exits with os.Exit so the Go test harness does not append
// non-JSON output to the protocol stream.
func TestAppServerFakeProcess(t *testing.T) {
	if os.Getenv("NECTAR_FAKE_APPSERVER") != "1" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	write := func(value any) {
		if err := encoder.Encode(value); err != nil {
			os.Exit(2)
		}
	}
	response := func(id json.RawMessage, result any) {
		write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	notify := func(method string, params any) {
		write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	}

	for {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if err := decoder.Decode(&request); err != nil {
			os.Exit(0)
		}
		switch request.Method {
		case "initialize":
			response(request.ID, map[string]any{})
		case "initialized":
			continue
		case "thread/start", "thread/resume":
			response(request.ID, map[string]any{"thread": map[string]any{"id": "thread-1"}})
		case "turn/start":
			response(request.ID, map[string]any{"turn": map[string]any{"id": "turn-1"}})
			notify("turn/started", map[string]any{
				"threadId": "thread-1",
				"turn":     map[string]any{"id": "turn-1", "status": "inProgress", "items": []any{}},
			})
		case "turn/steer":
			if got := fmt.Sprint(request.Params["expectedTurnId"]); got != "turn-1" {
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "bad turn id " + got}})
				continue
			}
			response(request.ID, map[string]any{"turnId": "turn-1"})
			notify("item/completed", map[string]any{
				"threadId":      "thread-1",
				"turnId":        "turn-1",
				"completedAtMs": 1,
				"item": map[string]any{
					"id":   "i1",
					"type": "agentMessage",
					"text": "steered: " + fmt.Sprint(request.Params["input"].([]any)[0].(map[string]any)["text"]),
				},
			})
			notify("turn/completed", map[string]any{
				"threadId": "thread-1",
				"turn":     map[string]any{"id": "turn-1", "status": "completed", "items": []any{}},
			})
		case "turn/interrupt":
			response(request.ID, map[string]any{})
		default:
			if len(request.ID) > 0 {
				response(request.ID, map[string]any{})
			}
		}
	}
}

func TestAppServerAdapterRejectsResumeWithoutSession(t *testing.T) {
	adapter := NewAppServerAdapter(Config{Executable: "codex", WorkingDir: t.TempDir()})
	if _, err := adapter.Resume(context.Background(), "", "hello", engine.EngineOptions{}); err == nil || !strings.Contains(err.Error(), "requires a session id") {
		t.Fatalf("resume error = %v", err)
	}
}
