package memory

import "context"

type Memory struct {
	ID      string
	Content string
	Tags    []string
}

type Store interface {
	Search(ctx context.Context, query string, limit int) ([]Memory, error)
	Put(ctx context.Context, memory Memory) error
}
