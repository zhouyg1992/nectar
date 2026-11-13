package core

import "sync"

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
}

type sessionUsage struct {
	usage         tokenUsage
	contextWindow int
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[SessionKey]string),
		usage:    make(map[SessionKey]sessionUsage),
	}
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
	delete(s.usage, key)
}

func (s *SessionStore) SetUsage(key SessionKey, usage *tokenUsage, contextWindow int) {
	if usage == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.usage == nil {
		s.usage = make(map[SessionKey]sessionUsage)
	}
	s.usage[key] = sessionUsage{usage: *usage, contextWindow: contextWindow}
}

func (s *SessionStore) Usage(key SessionKey) (tokenUsage, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	usage, ok := s.usage[key]
	return usage.usage, usage.contextWindow, ok
}
