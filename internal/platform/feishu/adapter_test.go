package feishu

import (
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/zhouyg1992/nectar/internal/attachment"
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

func TestToPlatformMessageParsesImage(t *testing.T) {
	msg := &channeltypes.NormalizedMessage{
		MessageID:      "m1",
		ChatID:         "c1",
		ChatType:       "p2p",
		Content:        "![image](img_1)",
		RawContentType: "image",
		Resources: []channeltypes.Resource{
			{Type: "image", FileKey: "img_1"},
		},
	}
	got := toPlatformMessage(msg)
	if got.Text != "" {
		t.Fatalf("text = %q, want empty", got.Text)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
	if got.Attachments[0].Kind != attachment.KindImage ||
		got.Attachments[0].Key != "img_1" ||
		got.Attachments[0].MessageID != "m1" {
		t.Fatalf("image attachment = %#v", got.Attachments[0])
	}
}

func TestToPlatformMessageParsesFile(t *testing.T) {
	msg := &channeltypes.NormalizedMessage{
		MessageID:      "m1",
		ChatID:         "c1",
		ChatType:       "p2p",
		Content:        `<file key="file_1" name="report.pdf"/>`,
		RawContentType: "file",
		Resources: []channeltypes.Resource{
			{Type: "file", FileKey: "file_1", FileName: "report.pdf"},
		},
	}
	got := toPlatformMessage(msg)
	if got.Text != "" {
		t.Fatalf("text = %q, want empty", got.Text)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
	item := got.Attachments[0]
	if item.Kind != attachment.KindFile ||
		item.Key != "file_1" ||
		item.Name != "report.pdf" ||
		item.MessageID != "m1" {
		t.Fatalf("file attachment = %#v", item)
	}
}

func TestToPlatformMessagePreservesRichPostImageOrder(t *testing.T) {
	msg := &channeltypes.NormalizedMessage{
		MessageID:      "m1",
		ChatID:         "c1",
		ChatType:       "p2p",
		Content:        "before ![image](img_1) middle ![image](img_2) after",
		RawContentType: "post",
		Resources: []channeltypes.Resource{
			{Type: "image", FileKey: "img_1"},
			{Type: "image", FileKey: "img_2"},
		},
	}
	got := toPlatformMessage(msg)
	if got.Text != "before [Image 1] middle [Image 2] after" {
		t.Fatalf("text = %q", got.Text)
	}
	if len(got.Attachments) != 2 || got.Attachments[0].Key != "img_1" || got.Attachments[1].Key != "img_2" {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
}

func TestToPlatformMessageKeepsTextWithFileAttachment(t *testing.T) {
	msg := &channeltypes.NormalizedMessage{
		MessageID:      "m1",
		ChatID:         "c1",
		ChatType:       "p2p",
		Content:        `please inspect <file key="file_1" name="report.pdf"/>`,
		RawContentType: "file",
		Resources: []channeltypes.Resource{
			{Type: "file", FileKey: "file_1", FileName: "report.pdf"},
		},
	}
	got := toPlatformMessage(msg)
	if got.Text != "please inspect" {
		t.Fatalf("text = %q", got.Text)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Key != "file_1" {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
}

func TestToPlatformMessageParsesFlatRichPost(t *testing.T) {
	msg := &channeltypes.NormalizedMessage{
		MessageID:      "m1",
		ChatID:         "c1",
		ChatType:       "p2p",
		Content:        "[rich text message]",
		RawContentType: "post",
		RawEvent: rawMessageEvent(
			"m1",
			"post",
			`{"title":"","content":[[{"tag":"text","text":"before"}],[{"tag":"img","image_key":"img_flat_1"}],[{"tag":"text","text":"after"}]]}`,
		),
	}
	got := toPlatformMessage(msg)
	if got.Text != "before\n[Image 1]\nafter" {
		t.Fatalf("text = %q", got.Text)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
	item := got.Attachments[0]
	if item.Kind != attachment.KindImage || item.Key != "img_flat_1" || item.MessageID != "m1" {
		t.Fatalf("attachment = %#v", item)
	}
}

func TestToPlatformMessageParsesImageOnlyFlatRichPost(t *testing.T) {
	msg := &channeltypes.NormalizedMessage{
		MessageID:      "m1",
		ChatID:         "c1",
		ChatType:       "p2p",
		Content:        "[rich text message]",
		RawContentType: "post",
		RawEvent: rawMessageEvent(
			"m1",
			"post",
			`{"title":"","content":[[{"tag":"img","image_key":"img_only"}]]}`,
		),
	}
	got := toPlatformMessage(msg)
	if got.Text != "" {
		t.Fatalf("text = %q, want empty", got.Text)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Key != "img_only" {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
}

func TestToPlatformMessageParsesLocaleRichPostMarkdownImage(t *testing.T) {
	msg := &channeltypes.NormalizedMessage{
		MessageID:      "m1",
		ChatID:         "c1",
		ChatType:       "p2p",
		Content:        "[rich text message]",
		RawContentType: "post",
		RawEvent: rawMessageEvent(
			"m1",
			"post",
			`{"zh_cn":{"title":"","content_v2":[[{"tag":"md","text":"look ![image](img_md_1)"}]]}}`,
		),
	}
	got := toPlatformMessage(msg)
	if got.Text != "look [Image 1]" {
		t.Fatalf("text = %q", got.Text)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Key != "img_md_1" {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
}

func rawMessageEvent(messageID, messageType, content string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   &messageID,
				MessageType: &messageType,
				Content:     &content,
			},
		},
	}
}

func TestAttachmentExtension(t *testing.T) {
	tests := []struct {
		name     string
		resource attachment.Resource
		mimeType string
		want     string
	}{
		{"jpeg image", attachment.Resource{Kind: attachment.KindImage}, "image/jpeg", ".jpg"},
		{"png image", attachment.Resource{Kind: attachment.KindImage}, "image/png", ".png"},
		{"named file", attachment.Resource{Kind: attachment.KindFile, Name: "report.PDF"}, "application/octet-stream", ".pdf"},
		{"mime file", attachment.Resource{Kind: attachment.KindFile}, "text/plain", ".txt"},
		{"unknown file", attachment.Resource{Kind: attachment.KindFile}, "application/octet-stream", ".bin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := attachmentExtension(tc.resource, tc.mimeType); got != tc.want {
				t.Fatalf("attachmentExtension() = %q, want %q", got, tc.want)
			}
		})
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
