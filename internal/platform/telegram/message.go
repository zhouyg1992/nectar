package telegram

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/platform"
)

type update struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgMessage struct {
	MessageID       int64       `json:"message_id"`
	MessageThreadID int64       `json:"message_thread_id"`
	From            *tgUser     `json:"from"`
	Chat            tgChat      `json:"chat"`
	Date            int64       `json:"date"`
	Text            string      `json:"text"`
	Caption         string      `json:"caption"`
	Entities        []tgEntity  `json:"entities"`
	CaptionEntities []tgEntity  `json:"caption_entities"`
	Photo           []tgPhoto   `json:"photo"`
	Document        *tgDocument `json:"document"`
	Audio           *tgAudio    `json:"audio"`
	Voice           *tgVoice    `json:"voice"`
	Video           *tgVideo    `json:"video"`
	Animation       *tgFile     `json:"animation"`
	Sticker         *tgFile     `json:"sticker"`
	ReplyToMessage  *tgMessage  `json:"reply_to_message"`
	MediaGroupID    string      `json:"media_group_id"`
}

type tgUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

type tgChat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username"`
}

type tgEntity struct {
	Type   string  `json:"type"`
	Offset int     `json:"offset"`
	Length int     `json:"length"`
	User   *tgUser `json:"user"`
}

type tgPhoto struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type tgDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type tgAudio struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type tgVoice struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type tgVideo struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type tgFile struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type replyContext struct {
	chatID    int64
	threadID  int64
	userID    int64
	messageID int64
}

func (a *Adapter) parseMessage(raw *tgMessage) (platform.Message, string, error) {
	if raw == nil {
		return platform.Message{}, "", errors.New("empty message")
	}
	if raw.From == nil || raw.From.IsBot {
		return platform.Message{}, "", errors.New("message has no human sender")
	}
	if raw.Chat.ID == 0 {
		return platform.Message{}, "", errors.New("message has no chat id")
	}
	if raw.Date > 0 && time.Unix(raw.Date, 0).Before(a.startedAt.Add(-oldMessageGrace)) {
		return platform.Message{}, "", errors.New("message predates process start")
	}

	chatType, err := telegramChatType(raw.Chat.Type)
	if err != nil {
		return platform.Message{}, "", err
	}

	text := strings.TrimSpace(raw.Text)
	if text == "" {
		text = strings.TrimSpace(raw.Caption)
	}
	if chatType == "group" {
		command := commandTargetsBot(text, a.botUsername)
		mentioned := a.mentionsBot(raw, text)
		repliedToBot := raw.ReplyToMessage != nil &&
			raw.ReplyToMessage.From != nil &&
			raw.ReplyToMessage.From.ID == a.botID
		if !command && !mentioned && !repliedToBot {
			return platform.Message{}, "", errors.New("group message did not mention the bot")
		}
	}
	text = stripBotMention(text, a.botUsername)
	text = normalizeStartCommand(text)

	attachments := extractAttachments(raw)
	if text == "" && len(attachments) == 0 {
		return platform.Message{}, "", errors.New("unsupported telegram message type")
	}
	for i := range attachments {
		attachments[i].MessageID = strconv.FormatInt(raw.MessageID, 10)
	}

	threadID := ""
	if raw.MessageThreadID > 0 {
		threadID = strconv.FormatInt(raw.MessageThreadID, 10)
	}
	messageID := strconv.FormatInt(raw.MessageID, 10)
	chatID := strconv.FormatInt(raw.Chat.ID, 10)

	return platform.Message{
		Channel:     "telegram",
		MessageID:   messageID,
		ChatID:      chatID,
		ChatType:    chatType,
		UserID:      strconv.FormatInt(raw.From.ID, 10),
		Text:        text,
		Attachments: attachments,
		ReplyTo:     messageID,
		ThreadID:    threadID,
		ReplyContext: replyContext{
			chatID:    raw.Chat.ID,
			threadID:  raw.MessageThreadID,
			userID:    raw.From.ID,
			messageID: raw.MessageID,
		},
	}, raw.MediaGroupID, nil
}

func telegramChatType(value string) (string, error) {
	switch value {
	case "private":
		return "p2p", nil
	case "group", "supergroup":
		return "group", nil
	default:
		return "", fmt.Errorf("unsupported telegram chat type %q", value)
	}
}

func (a *Adapter) mentionsBot(raw *tgMessage, text string) bool {
	if a.botUsername != "" && mentionIndex(text, a.botUsername) >= 0 {
		return true
	}
	for _, entities := range [][]tgEntity{raw.Entities, raw.CaptionEntities} {
		for _, entity := range entities {
			if entity.Type == "text_mention" && entity.User != nil && entity.User.ID == a.botID {
				return true
			}
		}
	}
	return false
}

func commandTargetsBot(text, username string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	at := strings.Index(fields[0], "@")
	if at < 0 {
		return true
	}
	target := fields[0][at+1:]
	return username == "" || strings.EqualFold(target, username)
}

func normalizeStartCommand(text string) string {
	text = strings.TrimSpace(text)
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "/start") {
		return text
	}
	return "/help"
}

func stripBotMention(text, username string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/") {
		if fields := strings.Fields(text); len(fields) > 0 {
			if at := strings.Index(fields[0], "@"); at >= 0 &&
				(username == "" || strings.EqualFold(fields[0][at+1:], username)) {
				text = fields[0][:at] + strings.TrimPrefix(text, fields[0])
			}
		}
	}
	if username == "" {
		return strings.TrimSpace(text)
	}
	for index := mentionIndex(text, username); index >= 0; index = mentionIndex(text, username) {
		text = text[:index] + text[index+1+len(username):]
	}
	return strings.TrimSpace(text)
}

func mentionIndex(text, username string) int {
	needle := "@" + strings.ToLower(username)
	lower := strings.ToLower(text)
	offset := 0
	for offset < len(lower) {
		index := strings.Index(lower[offset:], needle)
		if index < 0 {
			return -1
		}
		index += offset
		end := index + len(needle)
		startsClean := index == 0 || !isUsernameChar(lower[index-1])
		endsClean := end >= len(lower) || !isUsernameChar(lower[end])
		if startsClean && endsClean {
			return index
		}
		offset = index + 1
	}
	return -1
}

func isUsernameChar(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= '0' && value <= '9' ||
		value == '_'
}

func extractAttachments(raw *tgMessage) []attachment.Resource {
	switch {
	case len(raw.Photo) > 0:
		photo := raw.Photo[len(raw.Photo)-1]
		return []attachment.Resource{{
			Kind: attachment.KindImage,
			Key:  photo.FileID,
			Name: fmt.Sprintf("photo-%d.jpg", raw.MessageID),
		}}
	case raw.Document != nil:
		return []attachment.Resource{{
			Kind: kindFromMime(raw.Document.MimeType),
			Key:  raw.Document.FileID,
			Name: attachmentName(raw.Document.FileName, raw.Document.MimeType, raw.MessageID),
		}}
	case raw.Audio != nil:
		return []attachment.Resource{{
			Kind: attachment.KindAudio,
			Key:  raw.Audio.FileID,
			Name: attachmentName(raw.Audio.FileName, raw.Audio.MimeType, raw.MessageID),
		}}
	case raw.Voice != nil:
		return []attachment.Resource{{
			Kind: attachment.KindAudio,
			Key:  raw.Voice.FileID,
			Name: fmt.Sprintf("voice-%d.ogg", raw.MessageID),
		}}
	case raw.Video != nil:
		return []attachment.Resource{{
			Kind: attachment.KindVideo,
			Key:  raw.Video.FileID,
			Name: attachmentName(raw.Video.FileName, raw.Video.MimeType, raw.MessageID),
		}}
	case raw.Animation != nil:
		return []attachment.Resource{{
			Kind: attachment.KindVideo,
			Key:  raw.Animation.FileID,
			Name: attachmentName(raw.Animation.FileName, raw.Animation.MimeType, raw.MessageID),
		}}
	case raw.Sticker != nil:
		return []attachment.Resource{{
			Kind: attachment.KindFile,
			Key:  raw.Sticker.FileID,
			Name: attachmentName(raw.Sticker.FileName, raw.Sticker.MimeType, raw.MessageID),
		}}
	default:
		return nil
	}
}

func kindFromMime(mimeType string) attachment.Kind {
	switch {
	case strings.HasPrefix(strings.ToLower(mimeType), "image/"):
		return attachment.KindImage
	case strings.HasPrefix(strings.ToLower(mimeType), "audio/"):
		return attachment.KindAudio
	case strings.HasPrefix(strings.ToLower(mimeType), "video/"):
		return attachment.KindVideo
	default:
		return attachment.KindFile
	}
}

func attachmentName(name, mimeType string, messageID int64) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name != "" && name != "." {
		return name
	}
	ext := extensionFromMime(mimeType)
	if ext == "" {
		ext = ".bin"
	}
	return fmt.Sprintf("attachment-%d%s", messageID, ext)
}

func extensionFromMime(mimeType string) string {
	switch strings.ToLower(strings.SplitN(mimeType, ";", 2)[0]) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/mpeg":
		return ".mp3"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "video/mp4":
		return ".mp4"
	case "application/pdf":
		return ".pdf"
	default:
		return ""
	}
}
