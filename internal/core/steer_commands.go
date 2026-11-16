package core

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/platform"
)

const steerTimeout = 3 * time.Second

// handleSteerCommand returns true when text is one of the explicit steering
// aliases. Ordinary messages never take this path.
func (r *Router) handleSteerCommand(key SessionKey, msg platform.Message, text string) bool {
	prompt, ok := commandArg(text, "/append", "/steer", "/ps", "/btw")
	if !ok {
		return false
	}
	if !r.canManageSessions(msg) {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Admin Only", "Only configured admin users can steer a running task in group chats.")
		return true
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /steer <message> (aliases: /append, /ps, /btw)")
		return true
	}

	session, ok := r.sessions.Active(key)
	if !ok {
		r.sendCommandResult(msg, platform.CardInfo, "🧭 No Running Task", "There is no running task in the active session. Send the message normally.")
		return true
	}

	var run *sessionRun
	for _, candidate := range r.runningForKey(key) {
		if candidate.sessionID == session.ID {
			copy := candidate
			run = &copy
			break
		}
	}
	if run == nil {
		r.sendCommandResult(msg, platform.CardInfo, "🧭 No Running Task", "There is no running task in the active session. Send the message normally.")
		return true
	}
	if strings.TrimSpace(run.engineSessionID) == "" {
		r.sendCommandResult(msg, platform.CardInfo, "🧭 Task Starting", "The task is still starting. Try steering again in a moment.")
		return true
	}

	steerer, ok := r.engine.(engine.Steerer)
	if !ok {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Steering Unsupported", "The current engine does not support steering a running turn.")
		return true
	}

	engineSessionID := run.engineSessionID
	go func() {
		ctx, cancel := context.WithTimeout(r.ctx, steerTimeout)
		defer cancel()
		if err := steerer.Steer(ctx, engineSessionID, prompt); err != nil {
			if errors.Is(err, engine.ErrNoActiveTurn) {
				r.sendCommandResult(msg, platform.CardInfo, "🧭 Turn Ended", "The running turn ended before the message could be inserted. Send it normally if it is still needed.")
				return
			}
			r.logger.Warn("steer failed", "chat", msg.ChatID, "engine_session", engineSessionID, "error", err)
			r.sendCommandResult(msg, platform.CardError, "⚠️ Steering Failed", "Could not insert the message into the running task.")
			return
		}
		r.sendCommandResult(msg, platform.CardInfo, "🧭 Steering", "Inserted into the running task.")
	}()
	return true
}
