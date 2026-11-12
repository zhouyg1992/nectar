package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/platform"
)

var (
	imageMarkdownPattern = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
	mediaMarkerPattern   = regexp.MustCompile(`<(?:file|folder|audio|video|sticker)\b[^>]*/>`)
	extensionPattern     = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)
	imageOnlyPattern     = regexp.MustCompile(`^(?:\s*\[Image \d+\]\s*)+$`)
)

type Config struct {
	AppID     string
	AppSecret string
	LogLevel  string
	// AllowedUsers limits DMs by open_id. Empty means any reachable user.
	AllowedUsers []string
	// AllowedChats limits group chats by chat_id. Empty means any group + @bot.
	AllowedChats []string
}

type Adapter struct {
	cfg    Config
	client *lark.Client
	ws     *larkws.Client
	ch     channeltypes.Channel
	logger larkcore.Logger
}

func New(cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, errors.New("feishu app_id and app_secret are required")
	}

	level := parseLogLevel(cfg.LogLevel)
	client := lark.NewClient(cfg.AppID, cfg.AppSecret, lark.WithLogLevel(level))
	// channel.NewChannel only registers its message handler when the ws client
	// already has an EventDispatcher. Without this, the socket connects but
	// inbound messages are silently dropped.
	eventHandler := dispatcher.NewEventDispatcher("", "")
	wsClient := larkws.NewClient(cfg.AppID, cfg.AppSecret,
		larkws.WithLogLevel(level),
		larkws.WithEventHandler(eventHandler),
	)

	policy := channeltypes.PolicyConfig{}
	if len(cfg.AllowedUsers) > 0 {
		policy.DMMode = "allowlist"
		policy.DMAllowlist = cfg.AllowedUsers
	}
	if len(cfg.AllowedChats) > 0 {
		policy.GroupAllowlist = cfg.AllowedChats
	}

	return &Adapter{
		cfg:    cfg,
		client: client,
		ws:     wsClient,
		ch:     channel.NewChannel(client, wsClient, channeltypes.WithPolicyConfig(policy)),
		logger: larkcore.NewEventLogger(),
	}, nil
}

func (a *Adapter) Name() string { return "feishu" }

func (a *Adapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Streaming:   true,
		Card:        true,
		Media:       true,
		Group:       true,
		MentionOnly: true,
	}
}

func (a *Adapter) Start(ctx context.Context, handler func(platform.Message)) error {
	a.ch.OnReady(func() {
		a.logger.Info(ctx, "[Nectar] feishu websocket connected; ready to receive messages")
	})
	a.ch.OnReconnecting(func() {
		a.logger.Warn(ctx, "[Nectar] feishu websocket reconnecting")
	})
	a.ch.OnReconnected(func() {
		a.logger.Info(ctx, "[Nectar] feishu websocket reconnected")
	})
	a.ch.OnError(func(err error) {
		a.logger.Error(ctx, fmt.Sprintf("[Nectar] feishu channel error: %v", err))
	})
	a.ch.OnMessage(func(_ context.Context, msg *channeltypes.NormalizedMessage) error {
		normalized := toPlatformMessage(msg)
		a.logger.Info(ctx, fmt.Sprintf(
			"[Nectar] message received chat=%s type=%s raw_type=%s sender=%s text_len=%d mentioned_bot=%v resources=%d attachments=%d thread=%s",
			msg.ChatID, msg.ChatType, msg.RawContentType, msg.UserID, len(msg.Content), msg.MentionedBot, len(msg.Resources), len(normalized.Attachments), normalized.ThreadID,
		))
		handler(normalized)
		return nil
	})
	a.ch.OnReject(func(_ context.Context, event *channeltypes.RejectEvent) error {
		a.logger.Info(ctx, fmt.Sprintf(
			"[Nectar] ignored message chat=%s sender=%s reason=%s",
			event.ChatID, event.SenderID, event.Reason,
		))
		return nil
	})

	return a.ch.Start(ctx)
}

func (a *Adapter) Send(ctx context.Context, reply platform.Reply) (string, error) {
	if reply.ReplyInThread && reply.ReplyTo != "" {
		return a.sendThreadReply(ctx, reply)
	}

	var firstID string
	text := strings.TrimSpace(reply.Text)

	if text != "" {
		res, err := a.ch.Send(ctx, &channeltypes.SendInput{
			ChatID:         reply.ChatID,
			ReplyMessageID: reply.ReplyTo,
			Markdown:       text,
		})
		if err != nil {
			return "", fmt.Errorf("send text: %w", err)
		}
		firstID = res.MessageID
	}

	if reply.Card != nil {
		cardJSON, err := renderCard(reply.Card)
		if err != nil {
			return firstID, fmt.Errorf("encode card: %w", err)
		}
		res, err := a.ch.Send(ctx, &channeltypes.SendInput{
			ChatID:         reply.ChatID,
			ReplyMessageID: reply.ReplyTo,
			MsgType:        "interactive",
			Card:           cardJSON,
		})
		if err != nil {
			return firstID, fmt.Errorf("send card: %w", err)
		}
		if firstID == "" {
			firstID = res.MessageID
		}
	}

	for _, media := range reply.Media {
		id, err := a.sendMedia(ctx, reply.ChatID, reply.ReplyTo, media)
		if err != nil {
			return firstID, fmt.Errorf("send media: %w", err)
		}
		if firstID == "" {
			firstID = id
		}
	}

	if firstID == "" {
		return "", errors.New("empty reply")
	}
	return firstID, nil
}

func (a *Adapter) sendThreadReply(ctx context.Context, reply platform.Reply) (string, error) {
	var firstID string
	text := strings.TrimSpace(reply.Text)

	if text != "" {
		content, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			return "", fmt.Errorf("encode text: %w", err)
		}
		id, err := a.replyInThread(ctx, reply.ReplyTo, "text", string(content))
		if err != nil {
			return "", fmt.Errorf("reply text in thread: %w", err)
		}
		firstID = id
	}

	if reply.Card != nil {
		cardJSON, err := renderCard(reply.Card)
		if err != nil {
			return firstID, fmt.Errorf("encode card: %w", err)
		}
		id, err := a.replyInThread(ctx, reply.ReplyTo, "interactive", cardJSON)
		if err != nil {
			return firstID, fmt.Errorf("reply card in thread: %w", err)
		}
		if firstID == "" {
			firstID = id
		}
	}

	for _, media := range reply.Media {
		id, err := a.sendMedia(ctx, reply.ChatID, reply.ReplyTo, media)
		if err != nil {
			return firstID, fmt.Errorf("send media in thread: %w", err)
		}
		if firstID == "" {
			firstID = id
		}
	}

	if firstID == "" {
		return "", errors.New("empty reply")
	}
	return firstID, nil
}

func (a *Adapter) replyInThread(ctx context.Context, messageID, msgType, content string) (string, error) {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			ReplyInThread(true).
			Build()).
		Build()
	resp, err := a.client.Im.V1.Message.Reply(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", errors.New("feishu reply returned no message id")
	}
	return *resp.Data.MessageId, nil
}

func (a *Adapter) sendMedia(ctx context.Context, chatID, replyTo string, media platform.Media) (string, error) {
	input := &channeltypes.SendInput{
		ChatID:         chatID,
		ReplyMessageID: replyTo,
	}
	switch media.Type {
	case "image":
		input.MsgType = "image"
		input.ImagePath = media.Path
	default:
		input.MsgType = "file"
		input.FilePath = media.Path
	}
	res, err := a.ch.Send(ctx, input)
	if err != nil {
		return "", err
	}
	return res.MessageID, nil
}

func (a *Adapter) Update(ctx context.Context, messageID string, reply platform.Reply) error {
	if reply.Card == nil {
		return errors.New("feishu update requires a card")
	}
	content, err := renderCard(reply.Card)
	if err != nil {
		return fmt.Errorf("encode card: %w", err)
	}
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewPatchMessageReqBodyBuilder().Content(content).Build()).
		Build()
	resp, err := a.client.Im.V1.Message.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("patch feishu message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("patch feishu message: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (a *Adapter) Download(ctx context.Context, resource attachment.Resource, dir string) (attachment.Local, error) {
	if strings.TrimSpace(resource.Key) == "" {
		return attachment.Local{}, errors.New("feishu attachment key is empty")
	}
	mediaType := "file"
	if resource.Kind == attachment.KindImage {
		mediaType = "image"
	}
	data, err := a.downloadResource(ctx, resource, mediaType)
	if err != nil {
		return attachment.Local{}, fmt.Errorf("download feishu %s: %w", resource.Kind, err)
	}
	if len(data) == 0 {
		return attachment.Local{}, fmt.Errorf("download feishu %s: empty response", resource.Kind)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return attachment.Local{}, fmt.Errorf("create media dir: %w", err)
	}
	mimeType := http.DetectContentType(data)
	ext := attachmentExtension(resource, mimeType)
	f, err := os.CreateTemp(dir, "attachment-*"+ext)
	if err != nil {
		return attachment.Local{}, fmt.Errorf("create media file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return attachment.Local{}, fmt.Errorf("write media file: %w", err)
	}
	return attachment.Local{
		Kind:     resource.Kind,
		Path:     f.Name(),
		Name:     resource.Name,
		MimeType: mimeType,
	}, nil
}

func (a *Adapter) downloadResource(ctx context.Context, resource attachment.Resource, mediaType string) ([]byte, error) {
	if resource.MessageID != "" {
		req := larkim.NewGetMessageResourceReqBuilder().
			MessageId(resource.MessageID).
			FileKey(resource.Key).
			Type(mediaType).
			Build()
		resp, err := a.client.Im.V1.MessageResource.Get(ctx, req)
		if err == nil && resp != nil && resp.Success() && resp.File != nil {
			data, readErr := io.ReadAll(resp.File)
			if readErr == nil {
				return data, nil
			}
			err = readErr
		} else if err == nil && resp != nil {
			err = fmt.Errorf("code=%d msg=%s", resp.Code, resp.Msg)
		} else if err == nil {
			err = errors.New("empty message resource response")
		}
		if data, fallbackErr := a.ch.DownloadFile(ctx, resource.Key, mediaType); fallbackErr == nil {
			return data, nil
		} else {
			return nil, fmt.Errorf("message resource: %v; fallback: %w", err, fallbackErr)
		}
	}
	return a.ch.DownloadFile(ctx, resource.Key, mediaType)
}

func toPlatformMessage(msg *channeltypes.NormalizedMessage) platform.Message {
	text := msg.Content
	attachments := make([]attachment.Resource, 0, len(msg.Resources))
	for _, resource := range msg.Resources {
		kind, ok := attachmentKind(resource.Type, msg.RawContentType)
		if !ok || resource.FileKey == "" {
			continue
		}
		attachments = append(attachments, attachment.Resource{
			Kind:      kind,
			Key:       resource.FileKey,
			Name:      resource.FileName,
			MessageID: msg.MessageID,
		})
	}
	if rawText, rawAttachments, ok := parseRawInboundMessage(msg.RawEvent); ok {
		text = rawText
		attachments = rawAttachments
	}
	text = stripInboundMentions(text, msg.Mentions, msg.MentionAll)

	out := platform.Message{
		Channel:     "feishu",
		MessageID:   msg.MessageID,
		ChatID:      msg.ChatID,
		ChatType:    msg.ChatType,
		UserID:      msg.UserID,
		Text:        normalizeInboundText(text, attachments),
		Attachments: attachments,
		ReplyTo:     msg.MessageID,
	}
	if raw, ok := msg.RawEvent.(*larkim.P2MessageReceiveV1); ok &&
		raw.Event != nil && raw.Event.Message != nil && raw.Event.Message.ThreadId != nil {
		out.ThreadID = *raw.Event.Message.ThreadId
	}
	return out
}

func parseRawInboundMessage(raw any) (string, []attachment.Resource, bool) {
	event, ok := raw.(*larkim.P2MessageReceiveV1)
	if !ok || event.Event == nil || event.Event.Message == nil {
		return "", nil, false
	}
	message := event.Event.Message
	messageType := strings.ToLower(derefString(message.MessageType))
	content := derefString(message.Content)
	messageID := derefString(message.MessageId)

	switch messageType {
	case "text":
		var body struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &body); err != nil {
			return "", nil, false
		}
		return body.Text, nil, true
	case "post":
		return parseRawPost(content, messageID)
	case "image":
		var body struct {
			ImageKey string `json:"image_key"`
		}
		if err := json.Unmarshal([]byte(content), &body); err != nil || body.ImageKey == "" {
			return "", nil, false
		}
		return "", []attachment.Resource{{
			Kind:      attachment.KindImage,
			Key:       body.ImageKey,
			MessageID: messageID,
		}}, true
	case "file":
		var body struct {
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
		}
		if err := json.Unmarshal([]byte(content), &body); err != nil || body.FileKey == "" {
			return "", nil, false
		}
		return "", []attachment.Resource{{
			Kind:      attachment.KindFile,
			Key:       body.FileKey,
			Name:      body.FileName,
			MessageID: messageID,
		}}, true
	case "sticker":
		var body struct {
			FileKey string `json:"file_key"`
		}
		if err := json.Unmarshal([]byte(content), &body); err != nil || body.FileKey == "" {
			return "", nil, false
		}
		return "", []attachment.Resource{{
			Kind:      attachment.KindImage,
			Key:       body.FileKey,
			MessageID: messageID,
		}}, true
	}
	return "", nil, false
}

func parseRawPost(raw, messageID string) (string, []attachment.Resource, bool) {
	var content map[string]any
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		return "", nil, false
	}
	body := selectPostBody(content)
	if body == nil {
		return "", nil, false
	}

	paragraphs, _ := body["content_v2"].([]any)
	if len(paragraphs) == 0 {
		paragraphs, _ = body["content"].([]any)
	}

	attachments := make([]attachment.Resource, 0)
	seenFiles := make(map[string]struct{})
	addPostFiles(body, messageID, &attachments, seenFiles)
	addPostFiles(content, messageID, &attachments, seenFiles)
	imageNumber := 0
	addImage := func(key string) string {
		imageNumber++
		attachments = append(attachments, attachment.Resource{
			Kind:      attachment.KindImage,
			Key:       key,
			MessageID: messageID,
		})
		return fmt.Sprintf("[Image %d]", imageNumber)
	}

	lines := make([]string, 0, len(paragraphs)+1)
	if title := strings.TrimSpace(stringValue(body["title"])); title != "" {
		lines = append(lines, title)
	}
	for _, paragraphValue := range paragraphs {
		paragraph, ok := paragraphValue.([]any)
		if !ok {
			continue
		}
		var line strings.Builder
		for _, elementValue := range paragraph {
			element, ok := elementValue.(map[string]any)
			if !ok {
				continue
			}
			text := stringValue(element["text"])
			switch strings.ToLower(stringValue(element["tag"])) {
			case "md":
				line.WriteString(rewriteRawMarkdownImages(text, addImage))
			case "text":
				line.WriteString(text)
			case "a":
				href := stringValue(element["href"])
				switch {
				case href != "" && text != "":
					fmt.Fprintf(&line, "[%s](%s)", text, href)
				case href != "":
					line.WriteString(href)
				default:
					line.WriteString(text)
				}
			case "at":
				userID := stringValue(element["user_id"])
				userName := stringValue(element["user_name"])
				switch {
				case userID == "all" || userID == "all_members":
					line.WriteString("@all")
				case userName != "":
					line.WriteString("@" + userName)
				case userID != "":
					line.WriteString("@" + userID)
				}
			case "img":
				if key := stringValue(element["image_key"]); key != "" {
					line.WriteString(addImage(key))
				}
			case "media":
				if key := stringValue(element["file_key"]); key != "" {
					attachments = append(attachments, attachment.Resource{
						Kind:      attachment.KindFile,
						Key:       key,
						MessageID: messageID,
					})
					line.WriteString("[File]")
				}
			case "code_block":
				lang := stringValue(element["language"])
				fmt.Fprintf(&line, "\n```%s\n%s\n```\n", lang, text)
			case "hr":
				line.WriteString("\n---\n")
			default:
				line.WriteString(text)
			}
		}
		if strings.TrimSpace(line.String()) != "" {
			lines = append(lines, line.String())
		}
	}

	text := strings.TrimSpace(strings.Join(lines, "\n"))
	if imageOnlyPattern.MatchString(text) {
		text = ""
	}
	return text, attachments, true
}

func addPostFiles(content map[string]any, messageID string, attachments *[]attachment.Resource, seen map[string]struct{}) {
	files, _ := content["files"].([]any)
	for _, fileValue := range files {
		file, ok := fileValue.(map[string]any)
		if !ok || isTrue(file["is_folder"]) {
			continue
		}
		key := stringValue(file["file_key"])
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		*attachments = append(*attachments, attachment.Resource{
			Kind:      attachment.KindFile,
			Key:       key,
			Name:      stringValue(file["file_name"]),
			MessageID: messageID,
		})
	}
}

func selectPostBody(content map[string]any) map[string]any {
	if hasPostBody(content) {
		return content
	}
	for _, locale := range []string{"zh_cn", "en_us", "ja_jp"} {
		if body, ok := content[locale].(map[string]any); ok && hasPostBody(body) {
			return body
		}
	}
	keys := make([]string, 0, len(content))
	for key := range content {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if body, ok := content[key].(map[string]any); ok && hasPostBody(body) {
			return body
		}
	}
	return nil
}

func hasPostBody(content map[string]any) bool {
	if _, ok := content["content"].([]any); ok {
		return true
	}
	_, ok := content["content_v2"].([]any)
	return ok
}

func rewriteRawMarkdownImages(text string, addImage func(string) string) string {
	return imageMarkdownPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := imageMarkdownPattern.FindStringSubmatch(match)
		if len(parts) < 2 || !isFeishuImageKey(parts[1]) {
			return match
		}
		return addImage(parts[1])
	})
}

func isFeishuImageKey(key string) bool {
	return strings.HasPrefix(key, "img_") || strings.HasPrefix(key, "img_v")
}

func stripInboundMentions(text string, mentions []channeltypes.Mention, mentionAll bool) string {
	for _, mention := range mentions {
		if mention.Key != "" {
			text = strings.ReplaceAll(text, mention.Key, "")
		}
		if mention.IsBot && mention.Name != "" {
			text = strings.ReplaceAll(text, "@"+mention.Name, "")
		}
	}
	if mentionAll {
		text = strings.ReplaceAll(text, "@_all", "")
		text = strings.ReplaceAll(text, "@all", "")
	}
	return strings.TrimSpace(text)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func isTrue(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func attachmentKind(resourceType, rawContentType string) (attachment.Kind, bool) {
	switch strings.ToLower(resourceType) {
	case "image", "sticker":
		return attachment.KindImage, true
	case "file":
		return attachment.KindFile, true
	}
	switch strings.ToLower(rawContentType) {
	case "image":
		return attachment.KindImage, true
	case "file":
		return attachment.KindFile, true
	}
	return "", false
}

func normalizeInboundText(text string, attachments []attachment.Resource) string {
	text = strings.TrimSpace(text)
	if text == "" || len(attachments) == 0 {
		return text
	}

	if strings.TrimSpace(rewriteMediaMarkup(text, attachments, false)) == "" {
		return ""
	}
	text = strings.TrimSpace(rewriteMediaMarkup(text, attachments, true))
	if imageOnlyPattern.MatchString(text) {
		return ""
	}
	return text
}

func rewriteMediaMarkup(text string, attachments []attachment.Resource, placeholders bool) string {
	imageKeys := make(map[string]struct{})
	for _, item := range attachments {
		if item.Kind == attachment.KindImage {
			imageKeys[item.Key] = struct{}{}
		}
	}

	imageNumber := 0
	text = imageMarkdownPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := imageMarkdownPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		if _, ok := imageKeys[parts[1]]; !ok {
			return match
		}
		if !placeholders {
			return ""
		}
		imageNumber++
		return fmt.Sprintf("[Image %d]", imageNumber)
	})
	return mediaMarkerPattern.ReplaceAllString(text, "")
}

func attachmentExtension(resource attachment.Resource, mimeType string) string {
	mimeType = strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
	if resource.Kind == attachment.KindImage {
		switch mimeType {
		case "image/jpeg":
			return ".jpg"
		case "image/gif":
			return ".gif"
		case "image/webp":
			return ".webp"
		case "image/png":
			return ".png"
		}
	}

	name := filepath.Base(strings.TrimSpace(resource.Name))
	if name != "." && name != "" {
		if ext := strings.ToLower(filepath.Ext(name)); extensionPattern.MatchString(ext) {
			return ext
		}
	}

	switch mimeType {
	case "application/pdf":
		return ".pdf"
	case "application/json":
		return ".json"
	case "text/plain":
		return ".txt"
	}
	if resource.Kind == attachment.KindImage {
		return ".png"
	}
	return ".bin"
}

func parseLogLevel(value string) larkcore.LogLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return larkcore.LogLevelDebug
	case "warn", "warning":
		return larkcore.LogLevelWarn
	case "error":
		return larkcore.LogLevelError
	default:
		return larkcore.LogLevelInfo
	}
}

var _ platform.PlatformAdapter = (*Adapter)(nil)
