package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestSessionStoreLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	store, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Create(key, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(key, "second")
	if err != nil {
		t.Fatal(err)
	}
	if got := store.ActiveID(key); got != second.ID {
		t.Fatalf("active = %q, want %q", got, second.ID)
	}
	if items := store.List(key, false); len(items) != 2 {
		t.Fatalf("list length = %d, want 2", len(items))
	}

	if _, err := store.SetActive(key, first.ID); err != nil {
		t.Fatal(err)
	}
	if got := store.ActiveID(key); got != first.ID {
		t.Fatalf("active after switch = %q, want %q", got, first.ID)
	}
	if err := store.Rename(first.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEngineSession(first.ID, "engine-first"); err != nil {
		t.Fatal(err)
	}
	if err := store.Archive(key, second.ID); err != nil {
		t.Fatal(err)
	}
	if items := store.List(key, false); len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("active list = %#v", items)
	}
	if items := store.List(key, true); len(items) != 2 {
		t.Fatalf("all list length = %d, want 2", len(items))
	}

	reloaded, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := reloaded.GetSession(first.ID)
	if !ok {
		t.Fatal("renamed session missing after reload")
	}
	if session.Name != "renamed" || session.EngineSessionID != "engine-first" {
		t.Fatalf("reloaded session = %#v", session)
	}
	if got := reloaded.ActiveID(key); got != first.ID {
		t.Fatalf("reloaded active = %q, want %q", got, first.ID)
	}

	if err := reloaded.Archive(key, first.ID); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ActiveID(key); got != "" {
		t.Fatalf("active after archiving active session = %q", got)
	}
	next, err := reloaded.EnsureActive(key, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == first.ID || next.ID == second.ID {
		t.Fatalf("EnsureActive reused an archived session: %q", next.ID)
	}
}

func TestPersistentSessionStoreMigratesV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	legacy := `{
  "version": 1,
  "sessions": [
    {
      "channel": "feishu",
      "chat_id": "c1",
      "agent_id": "codex",
      "engine_session_id": "thread-old",
      "updated_at": "2026-11-15T10:00:00Z",
      "usage": {"input_tokens": 1, "output_tokens": 2, "context_tokens": 3, "context_window": 100}
    },
    {
      "channel": "feishu",
      "chat_id": "c1",
      "agent_id": "codex",
      "engine_session_id": "thread-new",
      "updated_at": "2026-11-15T11:00:00Z"
    }
  ]
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	if got, ok := store.Get(key); !ok || got != "thread-new" {
		t.Fatalf("migrated active engine session = %q, %v", got, ok)
	}
	items := store.List(key, true)
	if len(items) != 2 {
		t.Fatalf("migrated sessions = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.ID == "" || item.Name == "" || item.CreatedAt.IsZero() {
			t.Fatalf("migrated session missing metadata: %#v", item)
		}
	}

	activeID := store.ActiveID(key)
	if err := store.Touch(activeID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Version != sessionFileVersion {
		t.Fatalf("persisted version = %d, want %d", persisted.Version, sessionFileVersion)
	}
}

func TestSessionStoreAutoNameAndUsage(t *testing.T) {
	store := NewSessionStore()
	key := SessionKey{Channel: "telegram", ChatID: "42", AgentID: "codex"}
	session, err := store.EnsureActive(key, "")
	if err != nil {
		t.Fatal(err)
	}
	prompt := strings.Repeat("长任务 ", 20)
	if err := store.AutoName(session.ID, prompt); err != nil {
		t.Fatal(err)
	}
	named, ok := store.GetSession(session.ID)
	if !ok {
		t.Fatal("session missing")
	}
	if named.DisplayName() == "Untitled" || !strings.HasSuffix(named.DisplayName(), "…") {
		t.Fatalf("auto name = %q", named.DisplayName())
	}
	if err := store.IncrementMessageCount(session.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetUsageByID(session.ID, &tokenUsage{
		InputTokens:   10,
		OutputTokens:  5,
		ContextTokens: 15,
	}, 100); err != nil {
		t.Fatal(err)
	}
	usage, window, ok := store.UsageByID(session.ID)
	if !ok || usage.InputTokens != 10 || window != 100 {
		t.Fatalf("usage = %#v, %d, %v", usage, window, ok)
	}
	updated, _ := store.GetSession(session.ID)
	if updated.MessageCount != 1 || updated.ContextTokens != 15 {
		t.Fatalf("updated session = %#v", updated)
	}
}

func TestSessionStoreSortsByLastUsed(t *testing.T) {
	store := NewSessionStore()
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	first, err := store.Create(key, "first")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := store.Create(key, "second")
	if err != nil {
		t.Fatal(err)
	}
	items := store.List(key, false)
	if len(items) != 2 || items[0].ID != second.ID || items[1].ID != first.ID {
		t.Fatalf("list order = %#v", items)
	}
}
