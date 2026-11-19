package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFileStorePersistsAndSearches(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	item, err := store.Put(ctx, Memory{
		Kind:       KindProfile,
		Scope:      ScopeChat,
		ScopeID:    "chat:feishu/c1",
		Content:    "以后默认用中文回复",
		Tags:       []string{"language"},
		Status:     StatusActive,
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID == "" {
		t.Fatal("empty memory id")
	}

	reopened, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := reopened.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Content != item.Content || got.Status != StatusActive {
		t.Fatalf("reloaded memory = %#v, ok=%v", got, ok)
	}

	results, err := reopened.Search(ctx, SearchOptions{
		Query:    "中文",
		ScopeIDs: []string{"chat:feishu/c1"},
		Limit:    5,
		Now:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != item.ID {
		t.Fatalf("search results = %#v", results)
	}
}

func TestFileStoreSearchScopeStatusAndExpiry(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	expired := now.Add(-time.Hour)

	items := []Memory{
		{Kind: KindProfile, Scope: ScopeGlobal, Content: "全局偏好：中文", Status: StatusActive},
		{Kind: KindFact, Scope: ScopeChat, ScopeID: "chat:feishu/c1", Content: "项目使用中文文档", Status: StatusActive},
		{Kind: KindFact, Scope: ScopeProject, ScopeID: "project:nectar", Content: "Nectar 使用中文文档", Status: StatusActive},
		{Kind: KindFact, Scope: ScopeChat, ScopeID: "chat:feishu/c1", Content: "候选记忆", Status: StatusCandidate},
		{Kind: KindFact, Scope: ScopeChat, ScopeID: "chat:feishu/c2", Content: "其他会话", Status: StatusActive},
		{Kind: KindFact, Scope: ScopeChat, ScopeID: "chat:feishu/c1", Content: "中文但已过期", Status: StatusActive, ExpiresAt: &expired},
	}
	for _, item := range items {
		if _, err := store.Put(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	results, err := store.Search(ctx, SearchOptions{
		Query:    "中文",
		ScopeIDs: []string{"chat:feishu/c1", "project:nectar"},
		Limit:    10,
		Now:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool)
	for _, item := range results {
		got[item.Content] = true
	}
	for _, want := range []string{"全局偏好：中文", "项目使用中文文档", "Nectar 使用中文文档"} {
		if !got[want] {
			t.Fatalf("missing %q in %#v", want, got)
		}
	}
	for _, unwanted := range []string{"候选记忆", "其他会话", "中文但已过期"} {
		if got[unwanted] {
			t.Fatalf("unexpected %q in %#v", unwanted, got)
		}
	}

	candidates, err := store.Search(ctx, SearchOptions{
		Query:    "候选",
		Status:   StatusCandidate,
		ScopeIDs: []string{"chat:feishu/c1", "project:nectar"},
		Limit:    10,
		Now:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Content != "候选记忆" {
		t.Fatalf("candidate search results = %#v", candidates)
	}

	inactive, err := store.Search(ctx, SearchOptions{
		Query:           "候选",
		IncludeInactive: true,
		ScopeIDs:        []string{"chat:feishu/c1", "project:nectar"},
		Limit:           10,
		Now:             now,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundCandidate := false
	for _, item := range inactive {
		if item.Content == "候选记忆" && item.Status == StatusCandidate {
			foundCandidate = true
		}
	}
	if !foundCandidate {
		t.Fatalf("candidate missing from inactive search results = %#v", inactive)
	}
}

func TestFileStoreSetScope(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Put(ctx, Memory{
		Kind: KindFact, Scope: ScopeBot, Content: "scope can move", Status: StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetScope(ctx, item.ID, ScopeProject, "project:nectar"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Get(ctx, item.ID)
	if err != nil || !ok {
		t.Fatalf("get after project scope: ok=%v err=%v", ok, err)
	}
	if got.Scope != ScopeProject || got.ScopeID != "project:nectar" || got.ProjectID != "nectar" {
		t.Fatalf("project scope = %#v", got)
	}

	if err := store.SetScope(ctx, item.ID, ScopeChat, "chat:feishu/c1"); err != nil {
		t.Fatal(err)
	}
	got, _, err = store.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != ScopeChat || got.ScopeID != "chat:feishu/c1" || got.ProjectID != "" {
		t.Fatalf("chat scope = %#v", got)
	}

	if err := store.SetScope(ctx, item.ID, ScopeGlobal, ""); err != nil {
		t.Fatal(err)
	}
	got, _, err = store.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != ScopeBot || got.ScopeID != "" || got.ProjectID != "" {
		t.Fatalf("bot scope = %#v", got)
	}

	if err := store.SetScope(ctx, item.ID, ScopeProject, "nectar"); err == nil {
		t.Fatal("invalid project scope was accepted")
	}
	if err := store.SetScope(ctx, item.ID, ScopeChat, ""); err == nil {
		t.Fatal("empty chat scope was accepted")
	}
	if err := store.SetScope(ctx, item.ID, Scope("invalid"), ""); err == nil {
		t.Fatal("unknown scope was accepted")
	}
}

func TestFileStoreDeduplicatesAndUpdatesPin(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Put(ctx, Memory{
		Kind:    KindProfile,
		Scope:   ScopeChat,
		ScopeID: "chat:feishu/c1",
		Content: "默认用中文",
		Status:  StatusCandidate,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(ctx, Memory{
		Kind:    KindProfile,
		Scope:   ScopeChat,
		ScopeID: "chat:feishu/c1",
		Content: "默认用中文",
		Tags:    []string{"language"},
		Status:  StatusActive,
		Pinned:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("dedup ids = %q and %q", first.ID, second.ID)
	}
	all, err := store.List(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("memories = %d, want 1", len(all))
	}
	if all[0].Status != StatusActive || !all[0].Pinned || len(all[0].Tags) != 1 {
		t.Fatalf("updated memory = %#v", all[0])
	}

	if err := store.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Get(ctx, first.ID); err != nil || ok {
		t.Fatalf("deleted memory still present: ok=%v err=%v", ok, err)
	}
}

func TestFileStoreSeesWritesFromAnotherStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Put(ctx, Memory{
		Kind: KindProfile, Scope: ScopeChat, ScopeID: "chat:feishu/c1",
		Content: "first", Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Put(ctx, Memory{
		Kind: KindFact, Scope: ScopeChat, ScopeID: "chat:feishu/c1",
		Content: "second", Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	items, err := first.List(ctx, ListOptions{Status: StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("first store sees %d items, want 2", len(items))
	}
}

func TestFileStoreSoftDeleteAndTouch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Put(ctx, Memory{
		Kind: KindFact, Scope: ScopeChat, ScopeID: "chat:feishu/c1",
		Content: "remember me", Status: StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Touch(ctx, []string{item.ID}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Get(ctx, item.ID)
	if err != nil || !ok || got.UseCount != 1 || got.LastUsedAt == nil {
		t.Fatalf("touched memory = %#v ok=%v err=%v", got, ok, err)
	}
	if err := store.Delete(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Get(ctx, item.ID); err != nil || ok {
		t.Fatalf("deleted memory visible: ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "memory", "memory.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"deleted_at"`) {
		t.Fatal("soft delete did not leave a tombstone")
	}
}

func TestRedactAndNormalizeContent(t *testing.T) {
	if got := Redact("api_key=supersecret"); strings.Contains(got, "supersecret") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redacted content = %q", got)
	}
	workdir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	content, locality := NormalizeContent(workdir, "build in "+filepath.Join(workdir, "scripts"))
	if content != "build in ${PROJECT_ROOT}/scripts" {
		t.Fatalf("normalized content = %q", content)
	}
	if locality != LocalityTemplated {
		t.Fatalf("locality = %q", locality)
	}
	resolved, ok := ResolveContent(workdir, content)
	if !ok || resolved != "build in "+filepath.Join(workdir, "scripts") {
		t.Fatalf("resolved = %q ok=%v", resolved, ok)
	}
	if _, locality := NormalizeContent(workdir, `C:\Users\me\secret`); locality != LocalityDevice {
		t.Fatalf("device locality = %q", locality)
	}
}

func TestBotScopeAlwaysIncludedInSearch(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.Put(context.Background(), Memory{
		Kind:    KindProfile,
		Scope:   ScopeBot,
		Content: "用户喜欢吃苹果",
		Status:  StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.Search(context.Background(), SearchOptions{
		Query:    "苹果",
		ScopeIDs: []string{"chat:feishu/other"},
		Limit:    5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("items = %#v", items)
	}
}

func TestProjectIDUsesExplicitFile(t *testing.T) {
	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, ".nectar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workdir, ".nectar", "project-id"), []byte("my-project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ProjectID(workdir); got != "my-project" {
		t.Fatalf("project id = %q", got)
	}
	if got := ProjectScope(workdir); got != "project:my-project" {
		t.Fatalf("project scope = %q", got)
	}
}

func TestFileStoreConcurrentWriters(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const writers = 8
	const perWriter = 5
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			store, err := NewFileStore(dir)
			if err != nil {
				errs <- err
				return
			}
			for j := 0; j < perWriter; j++ {
				if _, err := store.Put(ctx, Memory{
					Kind: KindFact, Scope: ScopeChat, ScopeID: "chat:feishu/c1",
					Content: "writer-" + string(rune('a'+index)) + "-" + string(rune('0'+j)),
					Status:  StatusActive,
				}); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, ListOptions{Status: StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != writers*perWriter {
		t.Fatalf("items = %d, want %d", len(items), writers*perWriter)
	}
}

func TestFileStoreFeedbackUpdatesAndRanks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	scope := "chat:feishu/c1"

	good, err := store.Put(ctx, Memory{
		Kind: KindFact, Scope: ScopeChat, ScopeID: scope,
		Content: "部署使用 make release", Status: StatusActive, Confidence: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := store.Put(ctx, Memory{
		Kind: KindFact, Scope: ScopeChat, ScopeID: scope,
		Content: "部署使用 make release", Status: StatusActive, Confidence: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Put deduplicates identical content in the same scope, so create the second
	// entry in a different scope to compare feedback ranking.
	if bad.ID == good.ID {
		bad, err = store.Put(ctx, Memory{
			Kind: KindFact, Scope: ScopeProject, ScopeID: "project:nectar",
			Content: "部署使用 make release", Status: StatusActive, Confidence: 0.5,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := store.RecordFeedback(ctx, good.ID, FeedbackEvent{Event: FeedbackUseful, Source: "test"}); err != nil {
		t.Fatal(err)
	}
	updated, ok, err := store.Get(ctx, good.ID)
	if err != nil || !ok {
		t.Fatalf("get useful: ok=%v err=%v", ok, err)
	}
	if updated.UsefulCount != 1 || updated.Confidence <= 0.5 {
		t.Fatalf("useful feedback = %#v", updated)
	}
	if err := store.RecordFeedback(ctx, bad.ID, FeedbackEvent{Event: FeedbackHarmful, Source: "test"}); err != nil {
		t.Fatal(err)
	}
	updatedBad, _, err := store.Get(ctx, bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedBad.HarmfulCount != 1 || updatedBad.Confidence >= 0.5 {
		t.Fatalf("harmful feedback = %#v", updatedBad)
	}

	results, err := store.Search(ctx, SearchOptions{
		Query: "部署 make release", ScopeIDs: []string{scope, "project:nectar"}, Limit: 5, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 || results[0].ID != good.ID {
		t.Fatalf("feedback ranking = %#v", results)
	}

	data, err := os.ReadFile(filepath.Join(dir, "memory", "feedback.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), FeedbackUseful) || !strings.Contains(string(data), FeedbackHarmful) {
		t.Fatalf("feedback log = %s", data)
	}
}
