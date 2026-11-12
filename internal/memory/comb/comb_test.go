package comb

import (
	"context"
	"testing"
)

func TestFileStoreLifecycle(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := store.Record(ctx, Lesson{Condition: "when x", Action: "do y"}); err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Status != StatusPending || pending[0].ID == "" {
		t.Fatalf("pending = %#v", pending)
	}

	reloaded, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err = reloaded.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("persisted lessons = %d, want 1", len(pending))
	}

	if err := reloaded.Promote(ctx, pending[0].ID); err != nil {
		t.Fatal(err)
	}
	pending, err = reloaded.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("promoted lesson still pending: %#v", pending)
	}
}
