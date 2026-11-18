package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/zhouyg1992/nectar/internal/platform"
)

const completionReactionEmoji = "👍"

var _ platform.CompletionReactor = (*Adapter)(nil)

func (a *Adapter) AddCompletionReaction(ctx context.Context, messageID string, reply platform.Reply) error {
	chatID, err := strconv.ParseInt(strings.TrimSpace(reply.ChatID), 10, 64)
	if err != nil || chatID == 0 {
		return fmt.Errorf("telegram completion reaction has invalid chat id %q", reply.ChatID)
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(messageID), 10, 64)
	if err != nil || targetID == 0 {
		return fmt.Errorf("telegram completion reaction has invalid message id %q", messageID)
	}
	return a.call(ctx, "setMessageReaction", map[string]any{
		"chat_id":    chatID,
		"message_id": targetID,
		"reaction": []map[string]string{{
			"type":  "emoji",
			"emoji": completionReactionEmoji,
		}},
	}, nil)
}
