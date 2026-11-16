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
	if !isSessionManagementCommand(text) {
		return false
	}
	if !r.canManageSessions(msg) {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Admin Only", "Only configured admin users can manage sessions in group chats.")
		return true
	}

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
		includeArchived, limit, err := parseSessionListArgs(arg)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /list [all] [limit]")
			return true
		}
		r.sendCommandResult(msg, platform.CardInfo, "🌿 Sessions", r.sessionListText(key, includeArchived, limit))
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
		r.sendCommandResult(msg, platform.CardInfo, "🌿 Session Switched", sessionDetailText(session, r.engine.Name(), r.resolvedModel()))
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
		r.sendCommandResult(msg, platform.CardInfo, "✏️ Session Renamed", "Renamed to "+updated.DisplayName()+".")
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

	if ref, ok := commandArg(text, "/unarchive", "/restore"); ok {
		if ref == "" {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Usage", "Usage: /unarchive <id|name>")
			return true
		}
		session, err := r.findSessionIncludingArchived(key, ref)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Session Not Found", err.Error())
			return true
		}
		session, err = r.sessions.Unarchive(key, session.ID)
		if err != nil {
			r.sendCommandResult(msg, platform.CardError, "⚠️ Restore Failed", err.Error())
			return true
		}
		r.sendCommandResult(msg, platform.CardInfo, "📤 Session Restored", sessionDetailText(session, r.engine.Name(), r.resolvedModel()))
		return true
	}

	return false
}

func (r *Router) canManageSessions(msg platform.Message) bool {
	return !isGroupChat(msg) || r.isAdmin(msg.UserID)
}

func isSessionManagementCommand(text string) bool {
	for _, command := range []string{
		"/new", "/reset", "/list", "/sessions", "/switch", "/current",
		"/rename", "/archive", "/unarchive", "/restore",
	} {
		if _, ok := commandArg(text, command); ok {
			return true
		}
	}
	return false
}

func (r *Router) sessionListText(key SessionKey, includeArchived bool, limit int) string {
	sessions := r.sessions.List(key, includeArchived)
	if len(sessions) == 0 {
		return "No sessions yet. Send a message or /new."
	}
	total := len(sessions)
	if limit <= 0 {
		limit = 50
	}
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	activeID := r.sessions.ActiveID(key)
	var body strings.Builder
	body.WriteString("| # | Name | Session | Engine | Ctx | Last | State |\n")
	body.WriteString("|---|---|---|---|---|---|---|\n")
	number := 0
	for _, session := range sessions {
		index := "—"
		if session.Status == SessionStatusArchived {
			index = "—"
		} else {
			number++
			index = strconv.Itoa(number)
		}
		state := session.Status
		if session.ID == activeID {
			state = "active"
		}
		if r.isSessionRunning(session.ID) {
			state += " · running"
		}
		fmt.Fprintf(&body, "| %s | %s | %s | %s | %s | %s | %s |\n",
			index,
			markdownCell(sessionListName(session)),
			markdownCell(shortSessionID(session.ID)),
			markdownCell(session.AgentID),
			markdownCell(sessionContext(session)),
			markdownCell(formatSessionAge(session.LastUsedAt)),
			markdownCell(state),
		)
	}
	if total > len(sessions) {
		fmt.Fprintf(&body, "\n… showing %d of %d; use a larger /list limit for more.\n", len(sessions), total)
	}
	body.WriteString("\nUse /switch 1, /switch ID, or /switch NAME. /list all includes archived; /unarchive restores.")
	return strings.TrimSpace(body.String())
}

func parseSessionListArgs(arg string) (bool, int, error) {
	fields := strings.Fields(arg)
	includeArchived := false
	limit := 50
	if len(fields) > 0 && strings.EqualFold(fields[0], "all") {
		includeArchived = true
		fields = fields[1:]
	}
	if len(fields) > 0 {
		value, err := strconv.Atoi(fields[0])
		if err != nil || value < 1 || value > 200 {
			return false, 0, fmt.Errorf("limit must be between 1 and 200")
		}
		limit = value
		fields = fields[1:]
	}
	if len(fields) > 0 {
		return false, 0, fmt.Errorf("unexpected session list arguments")
	}
	return includeArchived, limit, nil
}

func (r *Router) findSession(key SessionKey, ref string) (Session, error) {
	return r.findSessionRef(key, ref, false)
}

func (r *Router) findSessionIncludingArchived(key SessionKey, ref string) (Session, error) {
	return r.findSessionRef(key, ref, true)
}

func (r *Router) findSessionRef(key SessionKey, ref string, includeArchived bool) (Session, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Session{}, fmt.Errorf("session reference is empty; use /list")
	}
	if strings.HasPrefix(ref, "#") {
		ref = strings.TrimPrefix(ref, "#")
	}
	if index, err := strconv.Atoi(ref); err == nil {
		if includeArchived {
			return Session{}, fmt.Errorf("numeric shortcuts only refer to non-archived sessions; use an id or name")
		}
		sessions := r.sessions.List(key, false)
		if index < 1 || index > len(sessions) {
			return Session{}, fmt.Errorf("session %d is out of range; use /list", index)
		}
		return sessions[index-1], nil
	}

	sessions := r.sessions.List(key, true)
	for _, session := range sessions {
		if session.ID == ref {
			if session.Status == SessionStatusArchived && !includeArchived {
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
		if prefix[0].Status == SessionStatusArchived && !includeArchived {
			return Session{}, fmt.Errorf("session %q is archived", prefix[0].ID)
		}
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		return Session{}, fmt.Errorf("session id prefix %q is ambiguous; use more characters", ref)
	}

	matches := make([]Session, 0, 1)
	for _, session := range sessions {
		if session.Status == SessionStatusArchived && !includeArchived {
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
	rows := [][2]string{
		{"Name", session.DisplayName()},
		{"ID", "`" + session.ID + "`"},
		{"Status", session.Status},
		{"Engine", engineName},
		{"Model", model},
		{"Session", engineSession},
		{"Context", sessionContext(session)},
		{"Messages", strconv.Itoa(session.MessageCount)},
		{"Created", formatSessionTime(session.CreatedAt)},
		{"Last used", formatSessionTime(session.LastUsedAt)},
	}
	return markdownKeyValue(rows)
}

func markdownKeyValue(rows [][2]string) string {
	var body strings.Builder
	body.WriteString("| Field | Value |\n")
	body.WriteString("|---|---|\n")
	for _, row := range rows {
		fmt.Fprintf(&body, "| %s | %s |\n", markdownCell(row[0]), markdownCell(row[1]))
	}
	return strings.TrimSpace(body.String())
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", `\|`)
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func sessionListName(session Session) string {
	name := session.DisplayName()
	runes := []rune(name)
	if len(runes) > 32 {
		return string(runes[:31]) + "…"
	}
	return name
}

func sessionContext(session Session) string {
	if session.ContextWindow <= 0 || session.ContextTokens <= 0 {
		return "ctx -"
	}
	percent := float64(session.ContextTokens) / float64(session.ContextWindow) * 100
	return fmt.Sprintf("ctx %s/%s (%.1f%%)",
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
