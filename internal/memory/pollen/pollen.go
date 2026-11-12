package pollen

import (
	"context"
	"time"
)

type SyncStatus struct {
	LastSync  time.Time
	Pending   int
	Conflicts []string
}

// SyncTarget is reserved for v1.3. Assets are comb lessons, AGENTS.md and skills.
type SyncTarget interface {
	Push(ctx context.Context, paths []string) error
	Pull(ctx context.Context, paths []string) error
	Status(ctx context.Context) (SyncStatus, error)
}
