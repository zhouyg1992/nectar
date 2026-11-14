package core

import "testing"

func TestAnchorStorePersists(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAnchorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := SessionKey{Channel: "feishu", ChatID: "chat-1", ThreadID: "thread-1", AgentID: "codex"}
	want := SessionAnchor{
		Channel:  "feishu",
		ChatID:   "chat-1",
		ThreadID: "thread-1",
		ChatType: "group",
		UserID:   "user-1",
	}
	if err := store.Set(key, want); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewAnchorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(key)
	if !ok {
		t.Fatal("anchor not restored")
	}
	if got != want {
		t.Fatalf("anchor = %#v, want %#v", got, want)
	}
}
