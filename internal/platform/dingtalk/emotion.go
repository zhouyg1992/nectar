package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	defaultTypingEmoji          = "🤔Thinking"
	customTextEmotionID         = "2659900"
	customTextEmotionBackground = "im_bg_1"
)

type textEmotion struct {
	EmotionID    string `json:"emotionId"`
	EmotionName  string `json:"emotionName"`
	Text         string `json:"text"`
	BackgroundID string `json:"backgroundId"`
}

type emotionRequest struct {
	RobotCode          string      `json:"robotCode"`
	OpenMsgID          string      `json:"openMsgId"`
	OpenConversationID string      `json:"openConversationId"`
	EmotionType        int         `json:"emotionType"`
	EmotionName        string      `json:"emotionName"`
	TextEmotion        textEmotion `json:"textEmotion"`
}

var _ platform.TypingIndicator = (*Adapter)(nil)

// StartTyping adds a lightweight thinking reaction to the user's message.
// It is used when streaming cards are unavailable, and is recalled when the
// agent turn finishes.
func (a *Adapter) StartTyping(ctx context.Context, replyCtx any) func() {
	rc, ok := replyCtx.(replyContext)
	if !ok || rc.messageID == "" || rc.conversationID == "" {
		return func() {}
	}
	if err := a.sendEmotion(ctx, rc, defaultTypingEmoji, false); err != nil {
		slog.Debug("dingtalk: add typing emotion failed", "error", err)
		return func() {}
	}
	return func() {
		recallCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.sendEmotion(recallCtx, rc, defaultTypingEmoji, true); err != nil {
			slog.Debug("dingtalk: recall typing emotion failed", "error", err)
		}
	}
}

func (a *Adapter) sendEmotion(ctx context.Context, rc replyContext, emoji string, recall bool) error {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return nil
	}
	robotCode := strings.TrimSpace(rc.robotCode)
	if robotCode == "" {
		robotCode = a.robotCode
	}
	if rc.messageID == "" || rc.conversationID == "" {
		return errors.New("dingtalk emotion requires messageID and conversationID")
	}
	token, err := a.getAccessToken(ctx)
	if err != nil {
		return err
	}
	path := "/v1.0/robot/emotion/reply"
	if recall {
		path = "/v1.0/robot/emotion/recall"
	}
	payload := emotionRequest{
		RobotCode:          robotCode,
		OpenMsgID:          rc.messageID,
		OpenConversationID: rc.conversationID,
		EmotionType:        2,
		EmotionName:        emoji,
		TextEmotion: textEmotion{
			EmotionID:    customTextEmotionID,
			EmotionName:  emoji,
			Text:         emoji,
			BackgroundID: customTextEmotionBackground,
		},
	}
	body, status, err := a.requestJSON(ctx, http.MethodPost, a.apiURL(path), payload, token, nil)
	if err != nil {
		return fmt.Errorf("dingtalk emotion request: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("dingtalk emotion returned status %d: %s", status, truncateBytes(body, 512))
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	var result struct {
		Success *bool `json:"success"`
	}
	if err := json.Unmarshal(body, &result); err == nil && result.Success != nil && !*result.Success {
		return errors.New("dingtalk emotion returned success=false")
	}
	return nil
}
