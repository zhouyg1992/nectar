package feishu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/zhouyg1992/nectar/internal/platform"
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
		a.logger.Info(ctx, fmt.Sprintf(
			"[Nectar] message received chat=%s type=%s sender=%s text_len=%d mentioned_bot=%v resources=%d",
			msg.ChatID, msg.ChatType, msg.UserID, len(msg.Content), msg.MentionedBot, len(msg.Resources),
		))
		handler(toPlatformMessage(msg))
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
		input := &channeltypes.SendInput{
			ChatID:         reply.ChatID,
			ReplyMessageID: reply.ReplyTo,
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
			return firstID, fmt.Errorf("send media: %w", err)
		}
		if firstID == "" {
			firstID = res.MessageID
		}
	}

	if firstID == "" {
		return "", errors.New("empty reply")
	}
	return firstID, nil
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

func (a *Adapter) Download(ctx context.Context, mediaKey string) (string, error) {
	data, err := a.ch.DownloadFile(ctx, mediaKey, "file")
	if err != nil {
		return "", fmt.Errorf("download feishu media: %w", err)
	}
	dir := filepath.Join(os.TempDir(), "nectar-media")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create media dir: %w", err)
	}
	f, err := os.CreateTemp(dir, "media-*")
	if err != nil {
		return "", fmt.Errorf("create media file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("write media file: %w", err)
	}
	return f.Name(), nil
}

func toPlatformMessage(msg *channeltypes.NormalizedMessage) platform.Message {
	text := msg.Content
	for _, mention := range msg.Mentions {
		if mention.Key != "" {
			text = strings.ReplaceAll(text, mention.Key, "")
		}
	}
	if msg.MentionAll {
		text = strings.ReplaceAll(text, "@_all", "")
	}

	out := platform.Message{
		Channel:   "feishu",
		MessageID: msg.MessageID,
		ChatID:    msg.ChatID,
		ChatType:  msg.ChatType,
		UserID:    msg.UserID,
		Text:      strings.TrimSpace(text),
		ReplyTo:   msg.MessageID,
	}
	for _, resource := range msg.Resources {
		if resource.FileKey != "" {
			out.MediaKeys = append(out.MediaKeys, resource.FileKey)
		}
	}
	return out
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
