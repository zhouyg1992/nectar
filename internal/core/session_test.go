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

func TestSessionStoreSeparatesBotsInSameChat(t *testing.T) {
	store := NewSessionStore()
	codexKey := SessionKey{BotID: "codex_dev", Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	claudeKey := SessionKey{BotID: "claude_review", Channel: "feishu", ChatID: "c1", AgentID: "claude"}
	codexSession, err := store.EnsureActive(codexKey, "")
	if err != nil {
		t.Fatal(err)
	}
	claudeSession, err := store.EnsureActive(claudeKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if codexSession.ID == claudeSession.ID {
		t.Fatalf("bots shared session %q", codexSession.ID)
	}
	if got, ok := store.Active(codexKey); !ok || got.ID != codexSession.ID {
		t.Fatalf("codex active = %#v, %v", got, ok)
	}
	if got, ok := store.Active(claudeKey); !ok || got.ID != claudeSession.ID {
		t.Fatalf("claude active = %#v, %v", got, ok)
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

func TestPersistentSessionStoreMigratesEngineBotIDToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	legacy := `{
  "version": 2,
  "sessions": [
    {
      "id": "ses_legacy",
      "channel": "feishu",
      "chat_id": "c1",
      "agent_id": "codex",
      "bot_id": "codex",
      "engine_session_id": "thread-old",
      "status": "active",
      "updated_at": "2026-11-16T10:00:00Z"
    }
  ],
  "active": [
    {
      "channel": "feishu",
      "chat_id": "c1",
      "agent_id": "codex",
      "bot_id": "codex",
      "session_id": "ses_legacy"
    }
  ]
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewPersistentSessionStore(path, "default")
	if err != nil {
		t.Fatal(err)
	}
	key := SessionKey{BotID: "default", Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	if got := store.ActiveID(key); got != "ses_legacy" {
		t.Fatalf("active session = %q, want ses_legacy", got)
	}
	session, ok := store.GetSession("ses_legacy")
	if !ok || session.BotID != "default" {
		t.Fatalf("migrated session = %#v, %v", session, ok)
	}
	if got, ok := store.Get(key); !ok || got != "thread-old" {
		t.Fatalf("engine session = %q, %v", got, ok)
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

func TestSessionStoreArchiveDoesNotReviveSession(t *testing.T) {
	store := NewSessionStore()
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	session, err := store.Create(key, "archived")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Archive(key, session.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetEngineSession(session.ID, "engine-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkStale(session.ID); err != nil {
		t.Fatal(err)
	}
	archived, ok := store.GetSession(session.ID)
	if !ok || archived.Status != SessionStatusArchived {
		t.Fatalf("archived session revived: %#v, ok=%v", archived, ok)
	}
	restored, err := store.Unarchive(key, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != SessionStatusActive {
		t.Fatalf("restored status = %q", restored.Status)
	}
	if got := store.ActiveID(key); got != session.ID {
		t.Fatalf("active after restore = %q, want %q", got, session.ID)
	}
}

func TestSessionStoreBeginTurnSanitizesAndCounts(t *testing.T) {
	store := NewSessionStore()
	key := SessionKey{Channel: "telegram", ChatID: "42", AgentID: "codex"}
	session, err := store.Create(key, "")
	if err != nil {
		t.Fatal(err)
	}
	prompt := "<at id=all></at> **secret** " + strings.Repeat("长任务 ", 20)
	if err := store.BeginTurn(session.ID, prompt); err != nil {
		t.Fatal(err)
	}
	updated, ok := store.GetSession(session.ID)
	if !ok {
		t.Fatal("session missing")
	}
	if updated.MessageCount != 1 {
		t.Fatalf("message count = %d", updated.MessageCount)
	}
	if strings.ContainsAny(updated.Name, "<>*") {
		t.Fatalf("session name still contains markup: %q", updated.Name)
	}
	if !strings.HasSuffix(updated.Name, "…") {
		t.Fatalf("long session name was not truncated: %q", updated.Name)
	}
	if err := store.FinishTurn(session.ID, &tokenUsage{
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
}

func TestSessionStoreSanitizesExplicitName(t *testing.T) {
	store := NewSessionStore()
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	session, err := store.Create(key, "<at id=all></at> **release**")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(session.Name, "<>*") {
		t.Fatalf("explicit session name still contains markup: %q", session.Name)
	}
}

func TestSessionStoreDetachedSessionKeepsActive(t *testing.T) {
	store := NewSessionStore()
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	active, err := store.EnsureActive(key, "")
	if err != nil {
		t.Fatal(err)
	}
	detached, err := store.CreateDetached(key, "relay:abcd1234")
	if err != nil {
		t.Fatal(err)
	}
	if detached.ID == active.ID {
		t.Fatal("CreateDetached reused the active session")
	}
	if got := store.ActiveID(key); got != active.ID {
		t.Fatalf("ActiveID() = %q, want %q", got, active.ID)
	}
	if listed := store.List(key, false); len(listed) != 2 {
		t.Fatalf("List() returned %d sessions, want 2", len(listed))
	}
	found, ok := store.FindByName(key, "relay:abcd1234")
	if !ok || found.ID != detached.ID {
		t.Fatalf("FindByName() = %q, %v; want %q", found.ID, ok, detached.ID)
	}
	if _, ok := store.FindByName(key, "missing"); ok {
		t.Fatal("FindByName returned a session for a missing name")
	}
	other := SessionKey{Channel: "feishu", ChatID: "c2", AgentID: "codex"}
	if _, ok := store.FindByName(other, "relay:abcd1234"); ok {
		t.Fatal("FindByName matched a session from another chat")
	}
	if err := store.Archive(key, detached.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.FindByName(key, "relay:abcd1234"); ok {
		t.Fatal("FindByName returned an archived session")
	}
}

func TestPersistentSessionStorePersistsDetachedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	store, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.EnsureActive(key, "")
	if err != nil {
		t.Fatal(err)
	}
	detached, err := store.CreateDetached(key, "relay:beef1234")
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewPersistentSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ActiveID(key); got != active.ID {
		t.Fatalf("ActiveID() after reload = %q, want %q", got, active.ID)
	}
	found, ok := reloaded.FindByName(key, "relay:beef1234")
	if !ok || found.ID != detached.ID {
		t.Fatalf("FindByName() after reload = %q, %v; want %q", found.ID, ok, detached.ID)
	}
}
