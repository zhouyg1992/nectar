package feishu

import (
	"context"
	"errors"
	"fmt"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const completionReactionEmoji = "DONE"

var _ platform.CompletionReactor = (*Adapter)(nil)

func (a *Adapter) AddCompletionReaction(ctx context.Context, messageID string, _ platform.Reply) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return errors.New("feishu completion reaction requires a message id")
	}
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(completionReactionEmoji).Build()).
			Build()).
		Build()
	resp, err := a.client.Im.V1.MessageReaction.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create feishu message reaction: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("create feishu message reaction: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}
