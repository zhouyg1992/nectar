package dingtalk

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
)

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	adapter, err := New(Config{
		ClientID:     "dingtest",
		ClientSecret: "secret",
		RobotCode:    "robot-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func callbackJSON(t *testing.T, values map[string]any) string {
	t.Helper()
	if _, ok := values["createAt"]; !ok {
		values["createAt"] = time.Now().UnixMilli()
	}
	body, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestParseCallbackText(t *testing.T) {
	adapter := testAdapter(t)
	raw := callbackJSON(t, map[string]any{
		"msgId":                     "msg-1",
		"conversationId":            "cid-1",
		"conversationType":          "2",
		"conversationTitle":         "team",
		"senderStaffId":             "staff-1",
		"senderNick":                "Alice",
		"sessionWebhook":            "https://example.com/webhook",
		"sessionWebhookExpiredTime": time.Now().Add(time.Hour).UnixMilli(),
		"msgtype":                   "text",
		"text":                      map[string]any{"content": "hello"},
		"isInAtList":                true,
	})

	msg, err := adapter.parseCallback(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Channel != "dingtalk" || msg.ChatID != "cid-1" || msg.UserID != "staff-1" {
		t.Fatalf("message = %#v", msg)
	}
	if msg.Text != "hello" || msg.ChatType != "group" {
		t.Fatalf("text/type = %q/%q", msg.Text, msg.ChatType)
	}
	rc, ok := msg.ReplyContext.(replyContext)
	if !ok {
		t.Fatalf("reply context type = %T", msg.ReplyContext)
	}
	if !rc.isGroup || rc.sessionWebhook == "" || rc.robotCode != "robot-test" {
		t.Fatalf("reply context = %#v", rc)
	}
}

func TestParseCallbackRichTextWithImages(t *testing.T) {
	adapter := testAdapter(t)
	raw := callbackJSON(t, map[string]any{
		"msgId":          "msg-2",
		"conversationId": "cid-2",
		"senderStaffId":  "staff-1",
		"msgtype":        "richText",
		"content": map[string]any{
			"richText": []map[string]any{
				{"text": "before "},
				{"text": " ", "downloadCode": "code-1"},
				{"text": " after"},
			},
		},
	})

	msg, err := adapter.parseCallback(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text != "before [Image 1] after" {
		t.Fatalf("text = %q", msg.Text)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %#v", msg.Attachments)
	}
	if msg.Attachments[0].Kind != attachment.KindImage || msg.Attachments[0].Key != "code-1" {
		t.Fatalf("attachment = %#v", msg.Attachments[0])
	}
}

func TestParseCallbackFile(t *testing.T) {
	adapter := testAdapter(t)
	raw := callbackJSON(t, map[string]any{
		"msgId":          "msg-3",
		"conversationId": "cid-3",
		"senderStaffId":  "staff-1",
		"msgtype":        "file",
		"content": map[string]any{
			"downloadCode": "code-2",
			"fileName":     "report.pdf",
		},
	})

	msg, err := adapter.parseCallback(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Kind != attachment.KindFile ||
		msg.Attachments[0].Name != "report.pdf" || msg.Attachments[0].Key != "code-2" {
		t.Fatalf("attachments = %#v", msg.Attachments)
	}
}

func TestParseCallbackQuote(t *testing.T) {
	adapter := testAdapter(t)
	raw := callbackJSON(t, map[string]any{
		"msgId":          "msg-4",
		"conversationId": "cid-4",
		"senderStaffId":  "staff-1",
		"msgtype":        "text",
		"text": map[string]any{
			"content":    "my reply",
			"isReplyMsg": true,
			"repliedMsg": map[string]any{
				"msgType": "text",
				"content": map[string]any{"text": "quoted text"},
			},
		},
	})

	msg, err := adapter.parseCallback(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "[引用消息]\n> quoted text\n\nmy reply"
	if msg.Text != want {
		t.Fatalf("text = %q, want %q", msg.Text, want)
	}
}

func TestParseCallbackDropsGroupWithoutMention(t *testing.T) {
	adapter := testAdapter(t)
	raw := callbackJSON(t, map[string]any{
		"msgId":            "msg-5",
		"conversationId":   "cid-5",
		"conversationType": "2",
		"senderStaffId":    "staff-1",
		"msgtype":          "text",
		"text":             map[string]any{"content": "hello"},
		"isInAtList":       false,
	})
	if _, err := adapter.parseCallback(raw); err == nil {
		t.Fatal("expected group message without mention to be ignored")
	}
}

func TestParseCallbackDropsOldMessage(t *testing.T) {
	adapter := testAdapter(t)
	raw := callbackJSON(t, map[string]any{
		"msgId":          "msg-6",
		"conversationId": "cid-6",
		"senderStaffId":  "staff-1",
		"msgtype":        "text",
		"text":           map[string]any{"content": "old"},
		"createAt":       int64(1),
	})
	if _, err := adapter.parseCallback(raw); err == nil {
		t.Fatal("expected old message to be ignored")
	}
}

func TestStripBotMention(t *testing.T) {
	env := callbackEnvelope{
		ChatbotUserID: "bot-1",
		AtUsers:       []atUser{{StaffID: "bot-1"}},
	}
	if got := stripBotMention("@Nectar /help", env); got != "/help" {
		t.Fatalf("got %q", got)
	}
	if got := stripBotMention("@alice please review", env); got != "@alice please review" {
		t.Fatalf("non-bot mention should be preserved, got %q", got)
	}
}

func TestMessageDedup(t *testing.T) {
	dedup := newMessageDedup(time.Minute)
	if dedup.duplicate("a") {
		t.Fatal("first message should not be duplicate")
	}
	if !dedup.duplicate("a") {
		t.Fatal("second message should be duplicate")
	}
	if dedup.duplicate("") {
		t.Fatal("empty id should never be duplicate")
	}
}

func TestExtractRichTextFallbackPictureCode(t *testing.T) {
	raw := json.RawMessage(fmt.Sprintf(`{"richText":[{"text":"x","pictureDownloadCode":%q}]}`, "legacy"))
	text, attachments := extractRichText(raw)
	if text != "x [Image 1]" || len(attachments) != 1 || attachments[0].Key != "legacy" {
		t.Fatalf("text=%q attachments=%#v", text, attachments)
	}
}
