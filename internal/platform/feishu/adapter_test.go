package feishu

import (
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestToPlatformMessageStripsMentions(t *testing.T) {
	msg := &channeltypes.NormalizedMessage{
		MessageID:  "m1",
		ChatID:     "c1",
		ChatType:   "group",
		UserID:     "u1",
		Content:    "@_user_1 hello @_all",
		Mentions:   []channeltypes.Mention{{Key: "@_user_1", Name: "nectar"}},
		MentionAll: true,
	}
	got := toPlatformMessage(msg)
	if got.Text != "hello" {
		t.Fatalf("text = %q, want %q", got.Text, "hello")
	}
	if got.MessageID != "m1" || got.ChatID != "c1" || got.Channel != "feishu" {
		t.Fatalf("message = %#v", got)
	}
}

func TestToPlatformMessageExtractsThreadID(t *testing.T) {
	threadID := "th1"
	msg := &channeltypes.NormalizedMessage{
		MessageID: "m1",
		ChatID:    "c1",
		ChatType:  "group",
		Content:   "hello",
		RawEvent: &larkim.P2MessageReceiveV1{
			Event: &larkim.P2MessageReceiveV1Data{
				Message: &larkim.EventMessage{ThreadId: &threadID},
			},
		},
	}
	got := toPlatformMessage(msg)
	if got.ThreadID != "th1" {
		t.Fatalf("thread id = %q, want th1", got.ThreadID)
	}
}

func TestNewWiresEventDispatcher(t *testing.T) {
	adapter, err := New(Config{AppID: "cli_test", AppSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.ws == nil || adapter.ws.EventHandler() == nil {
		t.Fatal("ws client has no event dispatcher; channel messages would be dropped")
	}
}
