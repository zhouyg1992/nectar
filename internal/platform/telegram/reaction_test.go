package telegram

import (
	"context"
	"testing"

	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestAddCompletionReaction(t *testing.T) {
	adapter, fake := newFakeAdapter(t, func(string, map[string]any) any {
		return true
	})
	if err := adapter.AddCompletionReaction(context.Background(), "99", platform.Reply{ChatID: "42"}); err != nil {
		t.Fatal(err)
	}
	calls := fake.callsFor("setMessageReaction")
	if len(calls) != 1 {
		t.Fatalf("setMessageReaction calls = %d", len(calls))
	}
	payload := calls[0].Payload
	if payload["chat_id"] != float64(42) || payload["message_id"] != float64(99) {
		t.Fatalf("reaction payload = %#v", payload)
	}
	reactions, _ := payload["reaction"].([]any)
	if len(reactions) != 1 {
		t.Fatalf("reaction payload = %#v", payload)
	}
	reaction, _ := reactions[0].(map[string]any)
	if reaction["type"] != "emoji" || reaction["emoji"] != completionReactionEmoji {
		t.Fatalf("reaction payload = %#v", payload)
	}
}
