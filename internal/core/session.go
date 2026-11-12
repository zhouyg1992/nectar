package core

import "sync"

// SessionKey is deliberately a triple so multiple engines and bots can share
// one chat without colliding. v1 always uses AgentID "codex".
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
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[SessionKey]string)}
}

func (s *SessionStore) Get(key SessionKey) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.sessions[key]
	return id, ok
}

func (s *SessionStore) Set(key SessionKey, engineSessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[key] = engineSessionID
}

func (s *SessionStore) Delete(key SessionKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}
