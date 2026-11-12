package core

import "testing"

func TestSessionStore(t *testing.T) {
	store := NewSessionStore()
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	if _, ok := store.Get(key); ok {
		t.Fatal("empty store returned a session")
	}
	store.Set(key, "thread-1")
	if got, ok := store.Get(key); !ok || got != "thread-1" {
		t.Fatalf("Get() = %q, %v", got, ok)
	}
	store.Delete(key)
	if _, ok := store.Get(key); ok {
		t.Fatal("Delete() did not remove the session")
	}
}
