package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionStore(t *testing.T) {
	store := NewSessionStore()
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	if _, ok := store.Get(key); ok {
		t.Fatal("empty store returned a session")
	}
	if err := store.Set(key, "thread-1"); err != nil {
		t.Fatal(err)
	}
	if got, ok := store.Get(key); !ok || got != "thread-1" {
		t.Fatalf("Get() = %q, %v", got, ok)
	}
	if err := store.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(key); ok {
		t.Fatal("Delete() did not remove the session")
	}
}

func TestPersistentSessionStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	key := SessionKey{Channel: "feishu", ChatID: "c1", ThreadID: "th1", AgentID: "codex"}

	store, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(key, "thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetUsage(key, &tokenUsage{
		InputTokens:       201,
		CachedInputTokens: 17280,
		OutputTokens:      16,
		ContextTokens:     17497,
	}, 996147); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.Get(key); !ok || got != "thread-1" {
		t.Fatalf("Get() = %q, %v", got, ok)
	}
	usage, window, ok := reloaded.Usage(key)
	if !ok || usage.InputTokens != 201 || usage.CachedInputTokens != 17280 ||
		usage.OutputTokens != 16 || usage.ContextTokens != 17497 || window != 996147 {
		t.Fatalf("Usage() = %#v, %d, %v", usage, window, ok)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sessions file mode = %o", info.Mode().Perm())
	}
}

func TestPersistentSessionStoreDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	store, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(key, "thread-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(key); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(key); ok {
		t.Fatal("deleted session was restored")
	}
}

func TestPersistentSessionStoreMissingFile(t *testing.T) {
	store, err := NewPersistentSessionStore(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(SessionKey{Channel: "feishu", ChatID: "c1"}); ok {
		t.Fatal("missing file produced a session")
	}
}

func TestPersistentSessionStoreRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentSessionStore(path); err == nil {
		t.Fatal("malformed sessions file was accepted")
	}
}
