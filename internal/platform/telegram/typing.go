package telegram

import (
	"context"
	"log/slog"
	"time"
)

const typingInterval = 4 * time.Second

func (a *Adapter) StartTyping(ctx context.Context, replyCtx any) func() {
	rc, ok := replyCtx.(replyContext)
	if !ok || rc.chatID == 0 {
		return func() {}
	}

	typingCtx, cancel := context.WithCancel(ctx)
	go func() {
		send := func() {
			if err := a.sendChatAction(typingCtx, rc.chatID, rc.threadID); err != nil &&
				typingCtx.Err() == nil {
				slog.Debug("telegram: send typing action failed", "error", err)
			}
		}
		send()
		ticker := time.NewTicker(typingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				send()
			}
		}
	}()
	return cancel
}
