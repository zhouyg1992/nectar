package dingtalk

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	quotedMessageLimit = 4000
	oldMessageGrace    = 2 * time.Second
)

var processStart = time.Now()

type callbackEnvelope struct {
	ConversationID            string          `json:"conversationId"`
	ConversationType          string          `json:"conversationType"`
	ConversationTitle         string          `json:"conversationTitle"`
	MsgID                     string          `json:"msgId"`
	CreateAt                  int64           `json:"createAt"`
	SenderStaffID             string          `json:"senderStaffId"`
	SenderID                  string          `json:"senderId"`
	SenderNick                string          `json:"senderNick"`
	SessionWebhook            string          `json:"sessionWebhook"`
	SessionWebhookExpiredTime int64           `json:"sessionWebhookExpiredTime"`
	Msgtype                   string          `json:"msgtype"`
	Text                      richTextContent `json:"text"`
	Content                   json.RawMessage `json:"content"`
	RobotCode                 string          `json:"robotCode"`
	ChatbotUserID             string          `json:"chatbotUserId"`
	IsInAtList                *bool           `json:"isInAtList"`
	AtUsers                   []atUser        `json:"atUsers"`
}

type atUser struct {
	DingtalkID string `json:"dingtalkId"`
	StaffID    string `json:"staffId"`
}

type richTextContent struct {
	Content    string          `json:"content"`
	IsReplyMsg bool            `json:"isReplyMsg"`
	RepliedMsg *repliedMessage `json:"repliedMsg"`
}

type repliedMessage struct {
	MsgType string          `json:"msgType"`
	Content json.RawMessage `json:"content"`
}

type replyContext struct {
	sessionWebhook            string
	sessionWebhookExpiredTime int64
	conversationID            string
	senderStaffID             string
	messageID                 string
	isGroup                   bool
	robotCode                 string
}

func (a *Adapter) parseCallback(raw string) (platform.Message, error) {
	var env callbackEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return platform.Message{}, fmt.Errorf("decode callback: %w", err)
	}
	if strings.TrimSpace(env.MsgID) == "" {
		return platform.Message{}, errors.New("callback has no msgId")
	}
	if isOldMessage(env.CreateAt) {
		return platform.Message{}, errors.New("message predates process start")
	}
	if env.ConversationType == "2" && env.IsInAtList != nil && !*env.IsInAtList {
		return platform.Message{}, errors.New("group message did not mention the bot")
	}

	isGroup := env.ConversationType == "2"
	robotCode := strings.TrimSpace(env.RobotCode)
	if robotCode == "" {
		robotCode = a.robotCode
	}

	var text string
	var attachments []attachment.Resource
	switch strings.ToLower(strings.TrimSpace(env.Msgtype)) {
	case "text":
		text = env.Text.Content
	case "richtext":
		text, attachments = extractRichText(env.Content)
	case "image", "picture":
		resource, err := extractDownloadAttachment(env.Content, attachment.KindImage, "image.png")
		if err != nil {
			return platform.Message{}, err
		}
		attachments = []attachment.Resource{resource}
	case "file":
		resource, err := extractDownloadAttachment(env.Content, attachment.KindFile, "attachment")
		if err != nil {
			return platform.Message{}, err
		}
		attachments = []attachment.Resource{resource}
	case "audio":
		text, attachments = extractAudio(env.Content)
	default:
		return platform.Message{}, fmt.Errorf("unsupported dingtalk message type %q", env.Msgtype)
	}

	if env.Text.IsReplyMsg && env.Text.RepliedMsg != nil {
		text = formatQuotedMessage(env.Text.RepliedMsg, text)
	}
	if isGroup {
		text = stripBotMention(text, env)
	}

	for i := range attachments {
		attachments[i].MessageID = env.MsgID
	}

	return platform.Message{
		Channel:     "dingtalk",
		MessageID:   env.MsgID,
		ChatID:      env.ConversationID,
		ChatType:    chatType(isGroup),
		UserID:      env.SenderStaffID,
		Text:        strings.TrimSpace(text),
		Attachments: attachments,
		ReplyTo:     env.MsgID,
		ReplyContext: replyContext{
			sessionWebhook:            env.SessionWebhook,
			sessionWebhookExpiredTime: env.SessionWebhookExpiredTime,
			conversationID:            env.ConversationID,
			senderStaffID:             env.SenderStaffID,
			messageID:                 env.MsgID,
			isGroup:                   isGroup,
			robotCode:                 robotCode,
		},
	}, nil
}

func chatType(isGroup bool) string {
	if isGroup {
		return "group"
	}
	return "p2p"
}

func isOldMessage(createAt int64) bool {
	if createAt <= 0 {
		return false
	}
	return time.UnixMilli(createAt).Before(processStart.Add(-oldMessageGrace))
}

func extractRichText(raw json.RawMessage) (string, []attachment.Resource) {
	var payload struct {
		RichText []struct {
			Text                string `json:"text"`
			DownloadCode        string `json:"downloadCode"`
			PictureDownloadCode string `json:"pictureDownloadCode"`
		} `json:"richText"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", nil
	}
	var text strings.Builder
	var attachments []attachment.Resource
	for _, part := range payload.RichText {
		if part.Text != "" {
			text.WriteString(part.Text)
		}
		code := strings.TrimSpace(part.DownloadCode)
		if code == "" {
			code = strings.TrimSpace(part.PictureDownloadCode)
		}
		if code == "" {
			continue
		}
		attachments = append(attachments, attachment.Resource{
			Kind: attachment.KindImage,
			Key:  code,
			Name: fmt.Sprintf("image-%d.png", len(attachments)+1),
		})
		current := strings.TrimRight(text.String(), " \t")
		text.Reset()
		text.WriteString(current)
		if current != "" {
			text.WriteString(" ")
		}
		text.WriteString(fmt.Sprintf("[Image %d]", len(attachments)))
	}
	return strings.TrimSpace(text.String()), attachments
}

func extractDownloadAttachment(raw json.RawMessage, kind attachment.Kind, fallbackName string) (attachment.Resource, error) {
	var payload struct {
		DownloadCode string `json:"downloadCode"`
		FileName     string `json:"fileName"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return attachment.Resource{}, fmt.Errorf("decode %s content: %w", kind, err)
	}
	code := strings.TrimSpace(payload.DownloadCode)
	if code == "" {
		return attachment.Resource{}, fmt.Errorf("%s message has no downloadCode", kind)
	}
	name := strings.TrimSpace(payload.FileName)
	if name == "" {
		name = fallbackName
	}
	return attachment.Resource{Kind: kind, Key: code, Name: name}, nil
}

func extractAudio(raw json.RawMessage) (string, []attachment.Resource) {
	var payload struct {
		DownloadCode string `json:"downloadCode"`
		Recognition  string `json:"recognition"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", nil
	}
	var attachments []attachment.Resource
	if strings.TrimSpace(payload.DownloadCode) != "" {
		attachments = append(attachments, attachment.Resource{
			Kind: attachment.KindAudio,
			Key:  strings.TrimSpace(payload.DownloadCode),
			Name: "audio.amr",
		})
	}
	return strings.TrimSpace(payload.Recognition), attachments
}

func formatQuotedMessage(replied *repliedMessage, userText string) string {
	quoted := extractQuotedText(replied.Content)
	if quoted == "" {
		return userText
	}
	quoted = truncateRunes(quoted, quotedMessageLimit)
	quoted = strings.ReplaceAll(quoted, "\r\n", "\n")
	quoted = strings.ReplaceAll(quoted, "\n", "\n> ")
	return "[引用消息]\n> " + quoted + "\n\n" + userText
}

func extractQuotedText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(findReadableText(value, 0))
}

func findReadableText(value any, depth int) string {
	if depth > 4 || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return ""
		}
		if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
			var nested any
			if json.Unmarshal([]byte(text), &nested) == nil {
				if found := findReadableText(nested, depth+1); found != "" {
					return found
				}
			}
		}
		return text
	case map[string]any:
		for _, key := range []string{"text", "content", "markdown", "title"} {
			if found := findReadableText(v[key], depth+1); found != "" {
				return found
			}
		}
		for _, item := range v {
			if found := findReadableText(item, depth+1); found != "" {
				return found
			}
		}
	case []any:
		var parts []string
		for _, item := range v {
			if found := findReadableText(item, depth+1); found != "" {
				parts = append(parts, found)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func stripBotMention(text string, env callbackEnvelope) string {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "@") {
		return text
	}
	rest := text
	if idx := strings.IndexAny(text, " \t\n"); idx >= 0 {
		rest = strings.TrimSpace(text[idx+1:])
	} else {
		rest = ""
	}
	// DingTalk normally removes the bot mention from text.content. When a
	// prefix is still present, only strip it for commands; otherwise a user's
	// own "@alice ..." mention would be lost.
	if strings.HasPrefix(rest, "/") {
		return rest
	}
	return text
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	if idx := strings.LastIndexByte(value[:cut], '\n'); idx > limit/2 {
		cut = idx
	}
	head := value[:cut]
	if strings.Count(head, "```")%2 == 1 {
		head += "\n```"
	}
	return head + "\n\n…（内容过长，已截断）"
}
