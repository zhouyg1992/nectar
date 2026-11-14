package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	minEditIntervalPrivate = 2 * time.Second
	minEditIntervalGroup   = 4 * time.Second
	maxSendAttempts        = 3
)

type sendMessageRequest struct {
	ChatID             int64               `json:"chat_id"`
	Text               string              `json:"text"`
	MessageThreadID    int64               `json:"message_thread_id,omitempty"`
	ParseMode          string              `json:"parse_mode,omitempty"`
	ReplyParameters    *replyParameters    `json:"reply_parameters,omitempty"`
	LinkPreviewOptions *linkPreviewOptions `json:"link_preview_options,omitempty"`
}

type editMessageRequest struct {
	ChatID             int64               `json:"chat_id"`
	MessageID          int64               `json:"message_id"`
	Text               string              `json:"text"`
	ParseMode          string              `json:"parse_mode,omitempty"`
	LinkPreviewOptions *linkPreviewOptions `json:"link_preview_options,omitempty"`
}

type chatActionRequest struct {
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
	Action          string `json:"action"`
}

type replyParameters struct {
	MessageID                int64 `json:"message_id"`
	AllowSendingWithoutReply bool  `json:"allow_sending_without_reply"`
}

type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

func (a *Adapter) Send(ctx context.Context, reply platform.Reply) (string, error) {
	if len(reply.Media) > 0 {
		return "", errors.New("telegram outbound media is not implemented yet")
	}

	chatID, err := strconv.ParseInt(strings.TrimSpace(reply.ChatID), 10, 64)
	if err != nil || chatID == 0 {
		return "", fmt.Errorf("telegram reply has invalid chat id %q", reply.ChatID)
	}
	threadID, _ := strconv.ParseInt(strings.TrimSpace(reply.ThreadID), 10, 64)
	replyTo, _ := strconv.ParseInt(strings.TrimSpace(reply.ReplyTo), 10, 64)

	source := reply.Text
	if reply.Card != nil {
		source = cardSource(*reply.Card)
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return "", errors.New("telegram reply is empty")
	}

	chunks := renderTelegramChunks(source)
	if len(chunks) == 0 {
		return "", errors.New("telegram reply is empty")
	}

	var firstID string
	for index, chunk := range chunks {
		params := sendMessageRequest{
			ChatID:             chatID,
			Text:               chunk.html,
			MessageThreadID:    threadID,
			ParseMode:          "HTML",
			LinkPreviewOptions: &linkPreviewOptions{IsDisabled: true},
		}
		if index == 0 && replyTo != 0 {
			params.ReplyParameters = &replyParameters{
				MessageID:                replyTo,
				AllowSendingWithoutReply: true,
			}
		}
		id, err := a.sendRendered(ctx, params, chunk.plain)
		if err != nil {
			return firstID, fmt.Errorf("send telegram message: %w", err)
		}
		if firstID == "" {
			firstID = id
		}
	}
	return firstID, nil
}

func (a *Adapter) Update(ctx context.Context, messageID string, reply platform.Reply) error {
	chatID, err := strconv.ParseInt(strings.TrimSpace(reply.ChatID), 10, 64)
	if err != nil || chatID == 0 {
		return fmt.Errorf("telegram update has invalid chat id %q", reply.ChatID)
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(messageID), 10, 64)
	if err != nil || targetID == 0 {
		return fmt.Errorf("telegram update has invalid message id %q", messageID)
	}

	source := reply.Text
	if reply.Card != nil {
		source = cardSource(*reply.Card)
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}

	final := reply.Card == nil || reply.Card.Status != platform.CardThinking
	editKey := reply.ChatID + ":" + messageID
	if !a.allowEdit(editKey, reply.ChatType, final) {
		return nil
	}
	if !final {
		source = previewSource(source)
	}

	threadID, _ := strconv.ParseInt(strings.TrimSpace(reply.ThreadID), 10, 64)
	chunks := renderTelegramChunks(source)
	if len(chunks) == 0 {
		return nil
	}

	params := editMessageRequest{
		ChatID:             chatID,
		MessageID:          targetID,
		Text:               chunks[0].html,
		ParseMode:          "HTML",
		LinkPreviewOptions: &linkPreviewOptions{IsDisabled: true},
	}
	err = a.editRendered(ctx, params, chunks[0].plain)
	switch {
	case err == nil:
	case isNotModified(err):
		if len(chunks) == 1 {
			return nil
		}
	case isEditImpossible(err):
		slog.Debug("telegram: edit target unavailable; sending replacement messages", "message_id", messageID)
		return a.sendRemaining(ctx, chatID, threadID, 0, chunks)
	default:
		return fmt.Errorf("edit telegram message: %w", err)
	}

	if len(chunks) > 1 {
		if err := a.sendRemaining(ctx, chatID, threadID, 0, chunks[1:]); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) sendRemaining(
	ctx context.Context,
	chatID int64,
	threadID int64,
	replyTo int64,
	chunks []renderedChunk,
) error {
	for index, chunk := range chunks {
		params := sendMessageRequest{
			ChatID:             chatID,
			Text:               chunk.html,
			MessageThreadID:    threadID,
			ParseMode:          "HTML",
			LinkPreviewOptions: &linkPreviewOptions{IsDisabled: true},
		}
		if index == 0 && replyTo != 0 {
			params.ReplyParameters = &replyParameters{
				MessageID:                replyTo,
				AllowSendingWithoutReply: true,
			}
		}
		if _, err := a.sendRendered(ctx, params, chunk.plain); err != nil {
			return fmt.Errorf("send telegram continuation: %w", err)
		}
	}
	return nil
}

func (a *Adapter) sendRendered(
	ctx context.Context,
	params sendMessageRequest,
	plainFallback string,
) (string, error) {
	var result tgMessage
	err := a.withRateLimitRetry(ctx, func() error {
		return a.call(ctx, "sendMessage", params, &result)
	})
	if err != nil && isParseError(err) {
		params.Text = plainFallback
		params.ParseMode = ""
		err = a.withRateLimitRetry(ctx, func() error {
			return a.call(ctx, "sendMessage", params, &result)
		})
	}
	if err != nil {
		return "", err
	}
	if result.MessageID == 0 {
		return "", nil
	}
	return strconv.FormatInt(result.MessageID, 10), nil
}

func (a *Adapter) editRendered(ctx context.Context, params editMessageRequest, plainFallback string) error {
	err := a.withRateLimitRetry(ctx, func() error {
		return a.call(ctx, "editMessageText", params, nil)
	})
	if err != nil && isParseError(err) {
		params.Text = plainFallback
		params.ParseMode = ""
		err = a.withRateLimitRetry(ctx, func() error {
			return a.call(ctx, "editMessageText", params, nil)
		})
	}
	return err
}

func (a *Adapter) sendChatAction(ctx context.Context, chatID, threadID int64) error {
	return a.call(ctx, "sendChatAction", chatActionRequest{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Action:          "typing",
	}, nil)
}

func (a *Adapter) withRateLimitRetry(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < maxSendAttempts; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			var apiErr *apiError
			if !errors.As(err, &apiErr) || apiErr.Code != 429 {
				return err
			}
			if !sleepContext(ctx, retryDelay(apiErr.RetryAfter)) {
				return ctx.Err()
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (a *Adapter) allowEdit(editKey, chatType string, final bool) bool {
	a.editMu.Lock()
	defer a.editMu.Unlock()
	if final {
		delete(a.lastEdits, editKey)
		return true
	}
	interval := minEditIntervalPrivate
	if chatType == "group" {
		interval = minEditIntervalGroup
	}
	now := time.Now()
	if last, ok := a.lastEdits[editKey]; ok && now.Sub(last) < interval {
		return false
	}
	a.lastEdits[editKey] = now
	if len(a.lastEdits) > 1024 {
		for id, at := range a.lastEdits {
			if now.Sub(at) > 10*time.Minute {
				delete(a.lastEdits, id)
			}
		}
	}
	return true
}

func isParseError(err error) bool {
	description := strings.ToLower(errorDescription(err))
	return strings.Contains(description, "can't parse entities") ||
		strings.Contains(description, "unsupported start tag") ||
		strings.Contains(description, "unmatched end tag") ||
		strings.Contains(description, "unexpected end tag") ||
		strings.Contains(description, "can't find end tag")
}

func isNotModified(err error) bool {
	return strings.Contains(strings.ToLower(errorDescription(err)), "message is not modified")
}

func isEditImpossible(err error) bool {
	description := strings.ToLower(errorDescription(err))
	return strings.Contains(description, "message to edit not found") ||
		strings.Contains(description, "message can't be edited") ||
		strings.Contains(description, "message identifier is not specified") ||
		strings.Contains(description, "message_id_invalid")
}

func errorDescription(err error) string {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.Description
	}
	return err.Error()
}
