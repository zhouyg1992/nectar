package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// SessionAnchor is the persisted delivery information for a chat. It is all a
// scheduled task needs to send a message when no inbound message is available.
type SessionAnchor struct {
	MessageID string `json:"message_id,omitempty"`
	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	ThreadID  string `json:"thread_id,omitempty"`
	ChatType  string `json:"chat_type,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

type anchorFile struct {
	Version int               `json:"version"`
	Anchors []persistedAnchor `json:"anchors"`
}

type persistedAnchor struct {
	MessageID string `json:"message_id,omitempty"`
	Channel   string `json:"channel"`
	ChatID    string `json:"chat_id"`
	ThreadID  string `json:"thread_id,omitempty"`
	AgentID   string `json:"agent_id"`
	ChatType  string `json:"chat_type,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

// AnchorStore persists the latest delivery target for each session key.
type AnchorStore struct {
	mu      sync.RWMutex
	path    string
	anchors map[SessionKey]SessionAnchor
}

func NewAnchorStore(dataDir string) (*AnchorStore, error) {
	store := &AnchorStore{anchors: make(map[SessionKey]SessionAnchor)}
	if dataDir == "" {
		return store, nil
	}
	store.path = filepath.Join(dataDir, "anchors.json")
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *AnchorStore) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read anchors: %w", err)
	}
	var file anchorFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse anchors: %w", err)
	}
	if file.Version != 0 && file.Version != 1 {
		return fmt.Errorf("unsupported anchors version %d", file.Version)
	}
	for _, item := range file.Anchors {
		if item.Channel == "" || item.ChatID == "" {
			continue
		}
		key := SessionKey{
			Channel:  item.Channel,
			ChatID:   item.ChatID,
			ThreadID: item.ThreadID,
			AgentID:  item.AgentID,
		}
		s.anchors[key] = SessionAnchor{
			Channel:   item.Channel,
			ChatID:    item.ChatID,
			ThreadID:  item.ThreadID,
			ChatType:  item.ChatType,
			UserID:    item.UserID,
			MessageID: item.MessageID,
		}
	}
	return nil
}

func (s *AnchorStore) Set(key SessionKey, anchor SessionAnchor) error {
	if anchor.Channel == "" || anchor.ChatID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.anchors[key]
	s.anchors[key] = anchor
	// MessageID changes on every inbound message. Keep it in memory for thread
	// replies, but only rewrite anchors.json when the delivery route changes.
	if ok &&
		existing.Channel == anchor.Channel &&
		existing.ChatID == anchor.ChatID &&
		existing.ThreadID == anchor.ThreadID &&
		existing.ChatType == anchor.ChatType &&
		existing.UserID == anchor.UserID {
		return nil
	}
	return s.persistLocked()
}

func (s *AnchorStore) Get(key SessionKey) (SessionAnchor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	anchor, ok := s.anchors[key]
	return anchor, ok
}

func (s *AnchorStore) List() []SessionAnchor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SessionAnchor, 0, len(s.anchors))
	for _, anchor := range s.anchors {
		out = append(out, anchor)
	}
	return out
}

func (s *AnchorStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	file := anchorFile{Version: 1, Anchors: make([]persistedAnchor, 0, len(s.anchors))}
	keys := make([]SessionKey, 0, len(s.anchors))
	for key := range s.anchors {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})
	for _, key := range keys {
		anchor := s.anchors[key]
		file.Anchors = append(file.Anchors, persistedAnchor{
			MessageID: anchor.MessageID,
			Channel:   key.Channel,
			ChatID:    key.ChatID,
			ThreadID:  key.ThreadID,
			AgentID:   key.AgentID,
			ChatType:  anchor.ChatType,
			UserID:    anchor.UserID,
		})
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal anchors: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create anchors directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".anchors-*.tmp")
	if err != nil {
		return fmt.Errorf("create anchors temp file: %w", err)
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
		return fmt.Errorf("chmod anchors temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write anchors: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close anchors temp file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace anchors: %w", err)
	}
	cleanup = false
	return nil
}
