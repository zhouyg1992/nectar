package memory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSearchBM25RanksRareTermFirst(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	scope := "chat:feishu/c1"

	items := []Memory{
		{Kind: KindProfile, Scope: ScopeChat, ScopeID: scope, Content: "用户喜欢用中文回复", Status: StatusActive},
		{Kind: KindProject, Scope: ScopeChat, ScopeID: scope, Content: "项目使用 Go 1.22，禁止 CGO", Status: StatusActive},
		{Kind: KindFact, Scope: ScopeChat, ScopeID: scope, Content: "部署脚本在 scripts/deploy.sh", Status: StatusActive},
	}
	for _, item := range items {
		if _, err := store.Put(ctx, item); err != nil {
			t.Fatal(err)
		}
	}

	results, err := store.Search(ctx, SearchOptions{
		Query: "Go CGO", ScopeIDs: []string{scope}, Limit: 3, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || !strings.Contains(results[0].Content, "CGO") {
		t.Fatalf("expected CGO memory first, got %#v", results)
	}
}

func TestSearchRankedExplainsAndBoostsTags(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	scope := "chat:feishu/c1"

	tagged, err := store.Put(ctx, Memory{
		Kind: KindFact, Scope: ScopeChat, ScopeID: scope,
		Content: "构建参数在配置文件中", Tags: []string{"cgo"}, Status: StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, Memory{
		Kind: KindFact, Scope: ScopeChat, ScopeID: scope,
		Content: "构建使用 make", Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.SearchRanked(ctx, SearchOptions{
		Query: "cgo", ScopeIDs: []string{scope}, Limit: 5, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].ID != tagged.ID {
		t.Fatalf("tagged memory should rank first: %#v", results)
	}
	if len(results[0].Why) == 0 || !strings.Contains(strings.Join(results[0].Why, ","), "tag:cgo") {
		t.Fatalf("missing tag explanation: %#v", results[0].Why)
	}
}

func TestTokenizeMemoryChineseAndCode(t *testing.T) {
	tokens := tokenizeMemory("项目使用 Go 1.22，禁止 CGO；脚本 scripts/deploy.sh")
	joined := strings.Join(tokens, " ")
	for _, want := range []string{"项目", "go", "1.22", "cgo", "scripts/deploy.sh"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tokens missing %q: %v", want, tokens)
		}
	}
}
