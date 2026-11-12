package feishu

import (
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
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

func TestNewWiresEventDispatcher(t *testing.T) {
	adapter, err := New(Config{AppID: "cli_test", AppSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.ws == nil || adapter.ws.EventHandler() == nil {
		t.Fatal("ws client has no event dispatcher; channel messages would be dropped")
	}
}
