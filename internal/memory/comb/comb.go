package comb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusDiscarded = "discarded"
)

// Lesson is a candidate piece of experience learned from a failed run.
// v1 only records pending lessons; injection and review cards come later.
type Lesson struct {
	ID        string    `json:"id"`
	Condition string    `json:"condition"`
	Action    string    `json:"action"`
	Evidence  float64   `json:"evidence"`
	Status    string    `json:"status"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Store interface {
	Record(ctx context.Context, lesson Lesson) error
	ListPending(ctx context.Context) ([]Lesson, error)
	Promote(ctx context.Context, id string) error
	Discard(ctx context.Context, id string) error
}

type FileStore struct {
	mu    sync.Mutex
	path  string
	items []Lesson
	// loaded is intentionally lazy so construction never fails on a missing file.
	loaded bool
}

func NewFileStore(dataDir string) (*FileStore, error) {
	if dataDir == "" {
		return nil, errors.New("data dir is empty")
	}
	dir := filepath.Join(dataDir, "comb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create comb dir: %w", err)
	}
	return &FileStore{path: filepath.Join(dir, "lessons.json")}, nil
}

func (s *FileStore) Record(ctx context.Context, lesson Lesson) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	if lesson.ID == "" {
		lesson.ID = newID()
	}
	if lesson.Status == "" {
		lesson.Status = StatusPending
	}
	if lesson.CreatedAt.IsZero() {
		lesson.CreatedAt = time.Now().UTC()
	}
	s.items = append(s.items, lesson)
	return s.persistLocked()
}

func (s *FileStore) ListPending(ctx context.Context) ([]Lesson, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	var out []Lesson
	for _, item := range s.items {
		if item.Status == StatusPending {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *FileStore) Promote(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, StatusActive)
}

func (s *FileStore) Discard(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, StatusDiscarded)
}

func (s *FileStore) setStatus(ctx context.Context, id, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Status = status
			return s.persistLocked()
		}
	}
	return fmt.Errorf("lesson %q not found", id)
}

func (s *FileStore) loadLocked() error {
	if s.loaded {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.loaded = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("read lessons: %w", err)
	}
	if len(data) == 0 {
		s.loaded = true
		return nil
	}
	if err := json.Unmarshal(data, &s.items); err != nil {
		return fmt.Errorf("decode lessons: %w", err)
	}
	s.loaded = true
	return nil
}

func (s *FileStore) persistLocked() error {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lessons: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write lessons: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace lessons: %w", err)
	}
	return nil
}

func newID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

var _ Store = (*FileStore)(nil)
