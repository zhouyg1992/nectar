package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/platform"
)

// handleSessionCommand returns true when text was a session command.
func (r *Router) handleSessionCommand(key SessionKey, msg platform.Message, text string) bool {
	if name, ok := commandArg(text, "/new", "/reset"); ok {
		session, err := r.sessions.Create(key, name)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Session Error", "Could not create a session: "+err.Error())
			return true
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌱 New Session", sessionCreatedText(session))
		return true
	}

	if arg, ok := commandArg(text, "/list", "/sessions"); ok {
		includeArchived := strings.EqualFold(strings.TrimSpace(arg), "all")
		if arg != "" && !includeArchived {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /list [all]")
			return true
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌿 Sessions", r.sessionListText(key, includeArchived))
		return true
	}

	if ref, ok := commandArg(text, "/switch"); ok {
		if ref == "" {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /switch <id|#|name>")
			return true
		}
		session, err := r.findSession(key, ref)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Session Not Found", err.Error())
			return true
		}
		session, err = r.sessions.SetActive(key, session.ID)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Switch Failed", err.Error())
			return true
		}
		r.sendCommandResult(msg, platform.CardInfo, "🔀 Session Switched", sessionDetailText(session, r.engine.Name(), r.resolvedModel()))
		return true
	}

	if strings.EqualFold(strings.TrimSpace(text), "/current") {
		session, ok := r.sessions.Active(key)
		if !ok {
			r.sendCommandResult(msg, platform.CardInfo, "🌿 Current Session", "No active session. Send a message or /new.")
			return true
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌿 Current Session", sessionDetailText(session, r.engine.Name(), r.resolvedModel()))
		return true
	}

	if name, ok := commandArg(text, "/rename"); ok {
		if name == "" {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /rename <name>")
			return true
		}
		session, ok := r.sessions.Active(key)
		if !ok {
			r.sendCommandResult(msg, platform.CardInfo, "✏️ Session Renamed", "No active session. Send a message or /new.")
			return true
		}
		if err := r.sessions.Rename(session.ID, name); err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Rename Failed", err.Error())
			return true
		}
		updated, _ := r.sessions.GetSession(session.ID)
		r.sendCommandResult(msg, platform.CardInfo, "✏️ Session Renamed", "Renamed to **"+updated.DisplayName()+"**.")
		return true
	}

	if ref, ok := commandArg(text, "/archive"); ok {
		session, err := r.sessionForArchive(key, ref)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Session Not Found", err.Error())
			return true
		}
		if err := r.sessions.Archive(key, session.ID); err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Archive Failed", err.Error())
			return true
		}
		body := "Archived **" + session.DisplayName() + "** (`" + session.ID + "`)."
		if r.sessions.ActiveID(key) == "" {
			body += "\n\nThe next message starts a new session."
		}
		r.sendCommandResult(msg, platform.CardInfo, "📦 Session Archived", body)
		return true
	}

	return false
}

func (r *Router) sessionListText(key SessionKey, includeArchived bool) string {
	sessions := r.sessions.List(key, includeArchived)
	if len(sessions) == 0 {
		return "No sessions yet. Send a message or /new."
	}
	if len(sessions) > 20 {
		sessions = sessions[:20]
	}
	activeID := r.sessions.ActiveID(key)
	lines := make([]string, 0, len(sessions)+2)
	for index, session := range sessions {
		marker := ""
		if session.ID == activeID {
			marker = " · active"
		}
		status := ""
		if session.Status != SessionStatusActive {
			status = " [" + session.Status + "]"
		}
		lines = append(lines, fmt.Sprintf("#%d  %s%s  ·  %s  ·  %s  ·  %s  ·  %s%s",
			index+1,
			session.DisplayName(),
			status,
			shortSessionID(session.ID),
			session.AgentID,
			sessionContext(session),
			formatSessionAge(session.LastUsedAt),
			marker,
		))
	}
	lines = append(lines, "", "Use /switch #1, /switch <id>, or /switch <name>. /list all includes archived sessions.")
	return strings.Join(lines, "\n")
}

func (r *Router) findSession(key SessionKey, ref string) (Session, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Session{}, fmt.Errorf("session reference is empty; use /list")
	}
	if strings.HasPrefix(ref, "#") {
		ref = strings.TrimPrefix(ref, "#")
	}
	if index, err := strconv.Atoi(ref); err == nil {
		sessions := r.sessions.List(key, false)
		if index < 1 || index > len(sessions) {
			return Session{}, fmt.Errorf("session #%d is out of range; use /list", index)
		}
		return sessions[index-1], nil
	}

	sessions := r.sessions.List(key, true)
	for _, session := range sessions {
		if session.ID == ref {
			if session.Status == SessionStatusArchived {
				return Session{}, fmt.Errorf("session %q is archived", ref)
			}
			return session, nil
		}
	}

	var prefix []Session
	for _, session := range sessions {
		if strings.HasPrefix(session.ID, ref) {
			prefix = append(prefix, session)
		}
	}
	if len(prefix) == 1 {
		if prefix[0].Status == SessionStatusArchived {
			return Session{}, fmt.Errorf("session %q is archived", prefix[0].ID)
		}
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		return Session{}, fmt.Errorf("session id prefix %q is ambiguous; use more characters", ref)
	}

	matches := make([]Session, 0, 1)
	for _, session := range sessions {
		if session.Status == SessionStatusArchived {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(session.Name), ref) {
			matches = append(matches, session)
		}
	}
	switch len(matches) {
	case 0:
		return Session{}, fmt.Errorf("session %q not found; use /list", ref)
	case 1:
		return matches[0], nil
	default:
		return Session{}, fmt.Errorf("session name %q is ambiguous; use an id or #number", ref)
	}
}

func (r *Router) sessionForArchive(key SessionKey, ref string) (Session, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		if session, ok := r.sessions.Active(key); ok {
			return session, nil
		}
		return Session{}, fmt.Errorf("no active session; use /list")
	}
	return r.findSession(key, ref)
}

func sessionCreatedText(session Session) string {
	return fmt.Sprintf("Started a new session: **%s**\nID: `%s`\n\nPrevious sessions are still available with /list.",
		session.DisplayName(), session.ID)
}

func sessionDetailText(session Session, engineName, model string) string {
	engineSession := session.EngineSessionID
	if engineSession == "" {
		engineSession = "not started"
	}
	lines := []string{
		"Name: " + session.DisplayName(),
		"ID: `" + session.ID + "`",
		"Status: " + session.Status,
		"Engine: " + engineName,
		"Model: " + model,
		"Session: " + engineSession,
		"Context: " + sessionContext(session),
		fmt.Sprintf("Messages: %d", session.MessageCount),
		"Created: " + formatSessionTime(session.CreatedAt),
		"Last used: " + formatSessionTime(session.LastUsedAt),
	}
	return strings.Join(lines, "\n")
}

func sessionContext(session Session) string {
	if session.ContextWindow <= 0 || session.ContextTokens <= 0 {
		return "ctx -"
	}
	percent := float64(session.ContextTokens) / float64(session.ContextWindow) * 100
	return fmt.Sprintf("ctx %s/%s (%.0f%%)",
		formatTokens(session.ContextTokens), formatTokens(session.ContextWindow), percent)
}

func shortSessionID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func formatSessionAge(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	age := time.Since(t)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	}
}

func formatSessionTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// commandArg matches an exact command or a command followed by a space. It is
// case-insensitive and preserves the argument's original casing.
func commandArg(text string, commands ...string) (string, bool) {
	text = strings.TrimSpace(text)
	for _, command := range commands {
		if strings.EqualFold(text, command) {
			return "", true
		}
		if len(text) <= len(command) || !strings.EqualFold(text[:len(command)], command) {
			continue
		}
		if text[len(command)] != ' ' && text[len(command)] != '\t' {
			continue
		}
		return strings.TrimSpace(text[len(command):]), true
	}
	return "", false
}
