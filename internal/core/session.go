package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/zhouyg1992/nectar/internal/memory"
)

const (
	sessionFileVersion = 2

	SessionStatusActive   = "active"
	SessionStatusArchived = "archived"
	SessionStatusStale    = "stale"

	maxSessionNameRunes     = 60
	maxAutoSessionNameRunes = 40
)

// SessionKey identifies the chat lane that owns a set of sessions. BotID
// separates bots that share the same platform chat; AgentID is kept for
// backwards compatibility and engine-level lanes.
type SessionKey struct {
	BotID    string
	Channel  string
	ChatID   string
	ThreadID string
	AgentID  string
}

func (k SessionKey) String() string {
	return k.effectiveBotID() + "\x00" + k.Channel + "\x00" + k.ChatID + "\x00" + k.ThreadID + "\x00" + k.AgentID
}

func (k SessionKey) effectiveBotID() string {
	if value := strings.TrimSpace(k.BotID); value != "" {
		return value
	}
	return strings.TrimSpace(k.AgentID)
}

func (k SessionKey) normalized() SessionKey {
	k.BotID = strings.TrimSpace(k.BotID)
	k.AgentID = strings.TrimSpace(k.AgentID)
	if k.BotID == k.AgentID {
		k.BotID = ""
	}
	return k
}

// Session is a Nectar-owned conversation. EngineSessionID is the underlying
// CLI session id and may be empty until the first agent turn starts.
type Session struct {
	ID              string
	Name            string
	Channel         string
	ChatID          string
	ThreadID        string
	BotID           string
	AgentID         string
	EngineSessionID string
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastUsedAt      time.Time
	MessageCount    int
	ContextTokens   int
	ContextWindow   int
	ParentID        string

	usage tokenUsage
}

func (s Session) DisplayName() string {
	if name := strings.TrimSpace(s.Name); name != "" {
		return name
	}
	return "Untitled"
}

type SessionStore struct {
	mu           sync.RWMutex
	sessions     map[string]*Session
	active       map[SessionKey]string
	path         string
	defaultBotID string
}

type sessionFile struct {
	Version  int                `json:"version"`
	Sessions []persistedSession `json:"sessions"`
	Active   []persistedActive  `json:"active,omitempty"`
}

type persistedSession struct {
	ID              string          `json:"id,omitempty"`
	Name            string          `json:"name,omitempty"`
	Channel         string          `json:"channel"`
	ChatID          string          `json:"chat_id"`
	ThreadID        string          `json:"thread_id,omitempty"`
	AgentID         string          `json:"agent_id"`
	BotID           string          `json:"bot_id,omitempty"`
	EngineSessionID string          `json:"engine_session_id,omitempty"`
	Status          string          `json:"status,omitempty"`
	CreatedAt       time.Time       `json:"created_at,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at,omitempty"`
	LastUsedAt      time.Time       `json:"last_used_at,omitempty"`
	MessageCount    int             `json:"message_count,omitempty"`
	ParentID        string          `json:"parent_id,omitempty"`
	Usage           *persistedUsage `json:"usage,omitempty"`
}

type persistedActive struct {
	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	ThreadID  string `json:"thread_id,omitempty"`
	BotID     string `json:"bot_id,omitempty"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
}

type persistedUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
	OutputTokens      int `json:"output_tokens"`
	ContextTokens     int `json:"context_tokens,omitempty"`
	ContextWindow     int `json:"context_window,omitempty"`
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		active:   make(map[SessionKey]string),
	}
}

// NewPersistentSessionStore loads sessions from path. Version 1 files are
// migrated on load; the next write persists the version 2 layout.
func NewPersistentSessionStore(path string, defaultBotID ...string) (*SessionStore, error) {
	store := NewSessionStore()
	store.path = path
	if len(defaultBotID) > 0 {
		store.defaultBotID = strings.TrimSpace(defaultBotID[0])
	}
	if path == "" {
		return store, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}
	var file sessionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse sessions: %w", err)
	}
	if file.Version > sessionFileVersion {
		return nil, fmt.Errorf("unsupported sessions version %d", file.Version)
	}

	now := time.Now().UTC()
	latest := make(map[SessionKey]Session)
	for _, item := range file.Sessions {
		if item.Channel == "" || item.ChatID == "" {
			continue
		}
		if item.EngineSessionID == "" && item.ID == "" {
			continue
		}
		if strings.TrimSpace(item.BotID) == "" {
			item.BotID = store.defaultBotID
			if item.BotID == "" {
				item.BotID = item.AgentID
			}
		}
		if store.defaultBotID != "" && strings.TrimSpace(item.BotID) == strings.TrimSpace(item.AgentID) {
			item.BotID = store.defaultBotID
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id, err = newSessionID()
			if err != nil {
				return nil, err
			}
		}
		created := item.CreatedAt
		if created.IsZero() {
			created = item.UpdatedAt
		}
		if created.IsZero() {
			created = now
		}
		updated := item.UpdatedAt
		if updated.IsZero() {
			updated = created
		}
		lastUsed := item.LastUsedAt
		if lastUsed.IsZero() {
			lastUsed = updated
		}
		name := cleanSessionName(item.Name)
		if name == "" && file.Version < sessionFileVersion {
			name = legacySessionName(updated)
		}
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = SessionStatusActive
		}
		session := &Session{
			ID:              id,
			Name:            name,
			Channel:         item.Channel,
			ChatID:          item.ChatID,
			ThreadID:        item.ThreadID,
			BotID:           item.BotID,
			AgentID:         item.AgentID,
			EngineSessionID: item.EngineSessionID,
			Status:          status,
			CreatedAt:       created,
			UpdatedAt:       updated,
			LastUsedAt:      lastUsed,
			MessageCount:    item.MessageCount,
			ParentID:        item.ParentID,
		}
		if item.Usage != nil {
			session.usage = tokenUsage{
				InputTokens:       item.Usage.InputTokens,
				CachedInputTokens: item.Usage.CachedInputTokens,
				OutputTokens:      item.Usage.OutputTokens,
				ContextTokens:     item.Usage.ContextTokens,
			}
			session.ContextTokens = item.Usage.ContextTokens
			session.ContextWindow = item.Usage.ContextWindow
		}
		store.sessions[id] = session

		key := item.key()
		if status != SessionStatusArchived {
			if previous, ok := latest[key]; !ok || lastUsed.After(previous.LastUsedAt) {
				latest[key] = *session
			}
		}
	}

	for _, item := range file.Active {
		botID := strings.TrimSpace(item.BotID)
		if botID == "" {
			botID = store.defaultBotID
			if botID == "" {
				botID = item.AgentID
			}
		}
		if store.defaultBotID != "" && botID == strings.TrimSpace(item.AgentID) {
			botID = store.defaultBotID
		}
		key := SessionKey{
			BotID:    botID,
			Channel:  item.Channel,
			ChatID:   item.ChatID,
			ThreadID: item.ThreadID,
			AgentID:  item.AgentID,
		}.normalized()
		session := store.sessions[item.SessionID]
		if session != nil && session.Status != SessionStatusArchived && session.matchesKey(key) {
			store.active[key] = item.SessionID
		}
	}
	for key, session := range latest {
		if _, ok := store.active[key]; !ok {
			store.active[key] = session.ID
		}
	}
	for key, id := range store.active {
		session := store.sessions[id]
		if session == nil || session.Status == SessionStatusArchived || !session.matchesKey(key) {
			delete(store.active, key)
		}
	}
	return store, nil
}

// Get returns the active engine session id. It keeps the original SessionStore
// contract used by the router and older tests.
func (s *SessionStore) Get(key SessionKey) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.activeLocked(key)
	if session == nil || session.EngineSessionID == "" {
		return "", false
	}
	return session.EngineSessionID, true
}

// Set updates the active session's engine id, creating a session if needed.
func (s *SessionStore) Set(key SessionKey, engineSessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.ensureActiveLocked(key, "")
	if err != nil {
		return err
	}
	session.EngineSessionID = engineSessionID
	session.Status = SessionStatusActive
	touchSession(session, time.Now().UTC())
	return s.persistLocked()
}

// Delete removes the active session for key. Other sessions in the same chat
// are preserved.
func (s *SessionStore) Delete(key SessionKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.active[key]
	if id == "" {
		return nil
	}
	delete(s.sessions, id)
	delete(s.active, key)
	return s.persistLocked()
}

func (s *SessionStore) SetUsage(key SessionKey, usage *tokenUsage, contextWindow int) error {
	if usage == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.ensureActiveLocked(key, "")
	if err != nil {
		return err
	}
	session.usage = *usage
	session.ContextTokens = usage.ContextTokens
	session.ContextWindow = contextWindow
	session.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

func (s *SessionStore) Usage(key SessionKey) (tokenUsage, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.activeLocked(key)
	if session == nil {
		return tokenUsage{}, 0, false
	}
	return session.usage, session.ContextWindow, true
}

// Create makes a new active session and keeps the previous session available
// in List.
func (s *SessionStore) Create(key SessionKey, name string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.createLocked(key, name)
	if err != nil {
		return Session{}, err
	}
	if err := s.persistLocked(); err != nil {
		delete(s.sessions, session.ID)
		delete(s.active, key)
		return Session{}, err
	}
	return *session, nil
}

// EnsureActive returns the active session, creating one when the lane is empty.
func (s *SessionStore) EnsureActive(key SessionKey, name string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.ensureActiveLocked(key, name)
	if err != nil {
		return Session{}, err
	}
	if session.ID == "" {
		return Session{}, fmt.Errorf("create session returned an empty id")
	}
	if session.Name == "" && cleanSessionName(name) != "" {
		session.Name = cleanSessionName(name)
		if err := s.persistLocked(); err != nil {
			return Session{}, err
		}
	}
	return *session, nil
}

func (s *SessionStore) ActiveID(key SessionKey) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active[key]
}

func (s *SessionStore) Active(key SessionKey) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.activeLocked(key)
	if session == nil {
		return Session{}, false
	}
	return *session, true
}

func (s *SessionStore) GetSession(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[id]
	if session == nil {
		return Session{}, false
	}
	return *session, true
}

func (s *SessionStore) List(key SessionKey, includeArchived bool) []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Session, 0)
	for _, session := range s.sessions {
		if !session.matchesKey(key) {
			continue
		}
		if !includeArchived && session.Status != SessionStatusActive && session.Status != SessionStatusStale {
			continue
		}
		items = append(items, *session)
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].LastUsedAt
		if left.IsZero() {
			left = items[i].UpdatedAt
		}
		right := items[j].LastUsedAt
		if right.IsZero() {
			right = items[j].UpdatedAt
		}
		if left.Equal(right) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return left.After(right)
	})
	return items
}

func (s *SessionStore) SetActive(key SessionKey, id string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return Session{}, fmt.Errorf("session %q not found", id)
	}
	if !session.matchesKey(key) {
		return Session{}, fmt.Errorf("session %q belongs to a different chat", id)
	}
	if session.Status == SessionStatusArchived {
		return Session{}, fmt.Errorf("session %q is archived", id)
	}
	s.active[key] = id
	touchSession(session, time.Now().UTC())
	if err := s.persistLocked(); err != nil {
		return Session{}, err
	}
	return *session, nil
}

func (s *SessionStore) Rename(id, name string) error {
	name = cleanSessionName(name)
	if name == "" {
		return fmt.Errorf("session name is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	session.Name = name
	session.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

func (s *SessionStore) Archive(key SessionKey, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	if !session.matchesKey(key) {
		return fmt.Errorf("session %q belongs to a different chat", id)
	}
	session.Status = SessionStatusArchived
	session.UpdatedAt = time.Now().UTC()
	if s.active[key] == id {
		delete(s.active, key)
	}
	return s.persistLocked()
}

// Unarchive restores an archived session. It becomes active when the lane has
// no other active session.
func (s *SessionStore) Unarchive(key SessionKey, id string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return Session{}, fmt.Errorf("session %q not found", id)
	}
	if !session.matchesKey(key) {
		return Session{}, fmt.Errorf("session %q belongs to a different chat", id)
	}
	session.Status = SessionStatusActive
	touchSession(session, time.Now().UTC())
	if s.active[key] == "" {
		s.active[key] = id
	}
	if err := s.persistLocked(); err != nil {
		return Session{}, err
	}
	return *session, nil
}

func (s *SessionStore) SetEngineSession(id, engineSessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	session.EngineSessionID = engineSessionID
	if session.Status != SessionStatusArchived {
		session.Status = SessionStatusActive
	}
	touchSession(session, time.Now().UTC())
	return s.persistLocked()
}

func (s *SessionStore) MarkStale(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	if session.Status == SessionStatusArchived {
		return nil
	}
	session.Status = SessionStatusStale
	session.EngineSessionID = ""
	session.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

// BeginTurn records a new turn and auto-names the session in one write.
func (s *SessionStore) BeginTurn(id, prompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	session.MessageCount++
	if strings.TrimSpace(session.Name) == "" {
		session.Name = deriveSessionName(prompt)
	}
	touchSession(session, time.Now().UTC())
	return s.persistLocked()
}

// FinishTurn records usage and activity in one write.
func (s *SessionStore) FinishTurn(id string, usage *tokenUsage, contextWindow int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	if usage != nil {
		session.usage = *usage
		session.ContextTokens = usage.ContextTokens
		session.ContextWindow = contextWindow
	}
	touchSession(session, time.Now().UTC())
	return s.persistLocked()
}

func (s *SessionStore) SetUsageByID(id string, usage *tokenUsage, contextWindow int) error {
	if usage == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	session.usage = *usage
	session.ContextTokens = usage.ContextTokens
	session.ContextWindow = contextWindow
	touchSession(session, time.Now().UTC())
	return s.persistLocked()
}

func (s *SessionStore) UsageByID(id string) (tokenUsage, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[id]
	if session == nil {
		return tokenUsage{}, 0, false
	}
	return session.usage, session.ContextWindow, true
}

func (s *SessionStore) IncrementMessageCount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	session.MessageCount++
	touchSession(session, time.Now().UTC())
	return s.persistLocked()
}

func (s *SessionStore) Touch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	touchSession(session, time.Now().UTC())
	return s.persistLocked()
}

func (s *SessionStore) AutoName(id, prompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil {
		return fmt.Errorf("session %q not found", id)
	}
	if strings.TrimSpace(session.Name) != "" {
		return nil
	}
	name := deriveSessionName(prompt)
	if name == "" {
		return nil
	}
	session.Name = name
	session.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

func (s *SessionStore) activeLocked(key SessionKey) *Session {
	id := s.active[key]
	if id == "" {
		return nil
	}
	session := s.sessions[id]
	if session == nil || session.Status == SessionStatusArchived || !session.matchesKey(key) {
		return nil
	}
	return session
}

func (s *SessionStore) ensureActiveLocked(key SessionKey, name string) (*Session, error) {
	if session := s.activeLocked(key); session != nil {
		return session, nil
	}
	session, err := s.createLocked(key, name)
	if err != nil {
		return nil, err
	}
	if err := s.persistLocked(); err != nil {
		delete(s.sessions, session.ID)
		delete(s.active, key)
		return nil, err
	}
	return session, nil
}

func (s *SessionStore) createLocked(key SessionKey, name string) (*Session, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	session := &Session{
		ID:         id,
		Name:       cleanSessionName(name),
		Channel:    key.Channel,
		ChatID:     key.ChatID,
		ThreadID:   key.ThreadID,
		BotID:      key.effectiveBotID(),
		AgentID:    key.AgentID,
		Status:     SessionStatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
		LastUsedAt: now,
	}
	s.sessions[id] = session
	s.active[key] = id
	return session, nil
}

func (s *SessionStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	file := sessionFile{
		Version:  sessionFileVersion,
		Sessions: make([]persistedSession, 0, len(s.sessions)),
		Active:   make([]persistedActive, 0, len(s.active)),
	}
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		session := s.sessions[id]
		item := persistedSession{
			ID:              session.ID,
			Name:            session.Name,
			Channel:         session.Channel,
			ChatID:          session.ChatID,
			ThreadID:        session.ThreadID,
			AgentID:         session.AgentID,
			BotID:           session.BotID,
			EngineSessionID: session.EngineSessionID,
			Status:          session.Status,
			CreatedAt:       session.CreatedAt,
			UpdatedAt:       session.UpdatedAt,
			LastUsedAt:      session.LastUsedAt,
			MessageCount:    session.MessageCount,
			ParentID:        session.ParentID,
		}
		if session.usage != (tokenUsage{}) || session.ContextWindow > 0 {
			item.Usage = &persistedUsage{
				InputTokens:       session.usage.InputTokens,
				CachedInputTokens: session.usage.CachedInputTokens,
				OutputTokens:      session.usage.OutputTokens,
				ContextTokens:     session.usage.ContextTokens,
				ContextWindow:     session.ContextWindow,
			}
		}
		file.Sessions = append(file.Sessions, item)
	}
	keys := make([]SessionKey, 0, len(s.active))
	for key := range s.active {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, key := range keys {
		id := s.active[key]
		if id == "" {
			continue
		}
		file.Active = append(file.Active, persistedActive{
			Channel:   key.Channel,
			ChatID:    key.ChatID,
			ThreadID:  key.ThreadID,
			BotID:     key.effectiveBotID(),
			AgentID:   key.AgentID,
			SessionID: id,
		})
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create sessions directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".sessions-*.tmp")
	if err != nil {
		return fmt.Errorf("create sessions temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod sessions temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write sessions: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync sessions: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close sessions temp file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace sessions: %w", err)
	}
	cleanup = false
	return nil
}

func (s Session) matchesKey(key SessionKey) bool {
	return s.Channel == key.Channel &&
		s.ChatID == key.ChatID &&
		s.ThreadID == key.ThreadID &&
		s.BotID == key.effectiveBotID() &&
		s.AgentID == key.AgentID
}

func touchSession(session *Session, now time.Time) {
	session.UpdatedAt = now
	session.LastUsedAt = now
}

func newSessionID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return "ses_" + hex.EncodeToString(raw[:]), nil
}

func cleanSessionName(name string) string {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	if name == "" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		switch r {
		case '<', '>', '`', '*', '_', '~', '[', ']', '(', ')', '\\', '|':
			return -1
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return ""
	}
	runes := []rune(name)
	if len(runes) > maxSessionNameRunes {
		name = string(runes[:maxSessionNameRunes]) + "…"
	}
	return name
}

func deriveSessionName(prompt string) string {
	prompt = memory.Redact(strings.TrimSpace(prompt))
	prompt = strings.Join(strings.Fields(prompt), " ")
	if prompt == "" || strings.HasPrefix(prompt, "/") {
		return ""
	}
	runes := []rune(prompt)
	if len(runes) > maxAutoSessionNameRunes {
		return cleanSessionName(string(runes[:maxAutoSessionNameRunes]) + "…")
	}
	return cleanSessionName(prompt)
}

func legacySessionName(updated time.Time) string {
	return "Session " + updated.Local().Format("Jan 02 15:04")
}

func (p persistedSession) key() SessionKey {
	return SessionKey{
		BotID:    p.BotID,
		Channel:  p.Channel,
		ChatID:   p.ChatID,
		ThreadID: p.ThreadID,
		AgentID:  p.AgentID,
	}.normalized()
}
