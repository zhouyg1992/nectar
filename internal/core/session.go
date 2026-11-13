package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// SessionKey is deliberately a tuple so multiple engines and bots can share
// one chat without colliding. AgentID is the configured engine name in v1.
type SessionKey struct {
	Channel  string
	ChatID   string
	ThreadID string
	AgentID  string
}

func (k SessionKey) String() string {
	return k.Channel + "\x00" + k.ChatID + "\x00" + k.ThreadID + "\x00" + k.AgentID
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[SessionKey]string
	usage    map[SessionKey]sessionUsage
	path     string
}

type sessionUsage struct {
	usage         tokenUsage
	contextWindow int
}

type sessionFile struct {
	Version  int                `json:"version"`
	Sessions []persistedSession `json:"sessions"`
}

type persistedSession struct {
	Channel         string          `json:"channel"`
	ChatID          string          `json:"chat_id"`
	ThreadID        string          `json:"thread_id,omitempty"`
	AgentID         string          `json:"agent_id"`
	EngineSessionID string          `json:"engine_session_id"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Usage           *persistedUsage `json:"usage,omitempty"`
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
		sessions: make(map[SessionKey]string),
		usage:    make(map[SessionKey]sessionUsage),
	}
}

// NewPersistentSessionStore loads sessions from path. A missing file starts an
// empty store; an unreadable or malformed file returns an error.
func NewPersistentSessionStore(path string) (*SessionStore, error) {
	store := NewSessionStore()
	store.path = path
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
	if file.Version != 0 && file.Version != 1 {
		return nil, fmt.Errorf("unsupported sessions version %d", file.Version)
	}
	for _, item := range file.Sessions {
		if item.Channel == "" || item.ChatID == "" || item.EngineSessionID == "" {
			continue
		}
		key := SessionKey{
			Channel:  item.Channel,
			ChatID:   item.ChatID,
			ThreadID: item.ThreadID,
			AgentID:  item.AgentID,
		}
		store.sessions[key] = item.EngineSessionID
		if item.Usage != nil {
			store.usage[key] = sessionUsage{
				usage: tokenUsage{
					InputTokens:       item.Usage.InputTokens,
					CachedInputTokens: item.Usage.CachedInputTokens,
					OutputTokens:      item.Usage.OutputTokens,
					ContextTokens:     item.Usage.ContextTokens,
				},
				contextWindow: item.Usage.ContextWindow,
			}
		}
	}
	return store, nil
}

func (s *SessionStore) Get(key SessionKey) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.sessions[key]
	return id, ok
}

func (s *SessionStore) Set(key SessionKey, engineSessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[key] = engineSessionID
	return s.persistLocked()
}

func (s *SessionStore) Delete(key SessionKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
	delete(s.usage, key)
	return s.persistLocked()
}

func (s *SessionStore) SetUsage(key SessionKey, usage *tokenUsage, contextWindow int) error {
	if usage == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.usage == nil {
		s.usage = make(map[SessionKey]sessionUsage)
	}
	s.usage[key] = sessionUsage{usage: *usage, contextWindow: contextWindow}
	return s.persistLocked()
}

func (s *SessionStore) Usage(key SessionKey) (tokenUsage, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	usage, ok := s.usage[key]
	return usage.usage, usage.contextWindow, ok
}

func (s *SessionStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	file := sessionFile{
		Version:  1,
		Sessions: make([]persistedSession, 0, len(s.sessions)),
	}
	keys := make([]SessionKey, 0, len(s.sessions))
	for key := range s.sessions {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})
	now := time.Now().UTC()
	for _, key := range keys {
		item := persistedSession{
			Channel:         key.Channel,
			ChatID:          key.ChatID,
			ThreadID:        key.ThreadID,
			AgentID:         key.AgentID,
			EngineSessionID: s.sessions[key],
			UpdatedAt:       now,
		}
		if usage, ok := s.usage[key]; ok {
			item.Usage = &persistedUsage{
				InputTokens:       usage.usage.InputTokens,
				CachedInputTokens: usage.usage.CachedInputTokens,
				OutputTokens:      usage.usage.OutputTokens,
				ContextTokens:     usage.usage.ContextTokens,
				ContextWindow:     usage.contextWindow,
			}
		}
		file.Sessions = append(file.Sessions, item)
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
