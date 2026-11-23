package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/memory"
	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestRouterRemembersAndInjectsMemory(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m1",
		Text:      "/remember 以后默认用中文回复",
	})
	waitForReplies(t, plat, 1)

	active, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("active memories = %d, want 1", len(active))
	}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m2",
		Text:      "hello",
	})
	waitForReplies(t, plat, 2)

	prompt, _ := eng.lastRequest()
	for _, want := range []string{"Nectar Memory", "以后默认用中文回复"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt = %q, want %q", prompt, want)
		}
	}
	active, err = memories.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].UseCount == 0 {
		t.Fatalf("memory usage was not tracked: %#v", active)
	}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m3",
		Text:      "/memory search 中文",
	})
	waitForReplies(t, plat, 3)
	if got := plat.lastReply(); !strings.Contains(got, "以后默认用中文回复") {
		t.Fatalf("search reply = %q", got)
	}
}

func TestRouterBotScopeMemoryIsSharedAcrossChats(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)
	router.cfg.MemoryDefaultScope = "bot"

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "dm",
		MessageID: "m1",
		Text:      "/remember 以后默认用中文回复",
	})
	waitForReplies(t, plat, 1)

	active, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Scope != memory.ScopeBot {
		t.Fatalf("active memories = %#v", active)
	}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "group",
		ChatType:  "group",
		MessageID: "m2",
		Text:      "hello",
	})
	waitForReplies(t, plat, 2)
	prompt, _ := eng.lastRequest()
	if !strings.Contains(prompt, "以后默认用中文回复") {
		t.Fatalf("group prompt missing bot-scoped memory: %q", prompt)
	}
}

func TestRouterDoesNotSendRecallMemoryCard(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &fakeEngine{}
	router, _, _ := newTestRouterWithMemory(t, eng, plat)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m1",
		Text:      "/remember 用户喜欢吃苹果",
	})
	waitForCardSends(t, plat, 1)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m2",
		Text:      "我喜欢什么水果",
	})
	waitForCardSends(t, plat, 2)

	plat.mu.Lock()
	defer plat.mu.Unlock()
	for _, card := range plat.sentCards {
		if card.Title == "🧠 Memory" && strings.Contains(card.Body, "Recalled for this turn") {
			t.Fatalf("recall card should not be sent: %#v", card)
		}
	}
}

func TestRouterMemoryForgetRequiresConfirmation(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m1",
		Text:      "/remember 用户喜欢吃苹果",
	})
	waitForReplies(t, plat, 1)
	active, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil || len(active) != 1 {
		t.Fatalf("active = %#v err=%v", active, err)
	}
	id := active[0].ID

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m2",
		Text:      "/memory forget " + id,
	})
	waitForReplies(t, plat, 2)
	if got := plat.lastReply(); !strings.Contains(got, "confirm") {
		t.Fatalf("confirmation reply = %q", got)
	}
	if _, ok, err := memories.Get(context.Background(), id); err != nil || !ok {
		t.Fatalf("memory deleted before confirmation: ok=%v err=%v", ok, err)
	}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m3",
		Text:      "/memory forget " + id + " confirm",
	})
	waitForReplies(t, plat, 3)
	if _, ok, err := memories.Get(context.Background(), id); err != nil || ok {
		t.Fatalf("memory not deleted after confirmation: ok=%v err=%v", ok, err)
	}
}

func TestFormatMemoryTableIncludesScope(t *testing.T) {
	table := formatMemoryTable([]memory.Memory{
		{ID: "id1", Kind: memory.KindProfile, Scope: memory.ScopeBot, Status: memory.StatusActive, Content: "用户喜欢吃苹果"},
		{ID: "id2", Kind: memory.KindFact, Scope: memory.ScopeChat, Status: memory.StatusCandidate, Content: "a|b"},
	})
	for _, want := range []string{"| ID | Kind | Scope | Status | Content |", "| bot |", "| chat |", "a\\|b"} {
		if !strings.Contains(table, want) {
			t.Fatalf("table missing %q:\n%s", want, table)
		}
	}
}

func TestRouterExtractsRuleCandidate(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1",
		Text: "以后默认用中文回复",
	})
	waitForReplies(t, plat, 1)

	var candidates []memory.Memory
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		candidates, err = memories.List(context.Background(), memory.ListOptions{Status: memory.StatusCandidate})
		if err != nil {
			t.Fatal(err)
		}
		if len(candidates) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(candidates) != 1 {
		t.Fatalf("rule candidates = %#v", candidates)
	}
	if candidates[0].Kind != memory.KindProfile || candidates[0].Content != "以后默认用中文回复" {
		t.Fatalf("rule candidate = %#v", candidates[0])
	}
}

func TestRouterSkipsTaskStateAndQuestionRuleCandidates(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)
	msg := platform.Message{Channel: "feishu", ChatID: "c1", UserID: "u1"}

	for _, text := range []string{
		"1. 头像是正常男性头像，不是无头像\n2. AppsFlyer 归因不是 NONE 或 organic",
		"帮我检查这个页面，必须过滤 NONE 和 organic",
		"cpu吃紧，我家SWAP是不是不起作用",
		"AppsFlyer 归因不是 NONE 或 organic",
	} {
		router.extractRuleMemories(msg, text, "")
	}

	candidates, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusCandidate})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("task state produced candidates = %#v", candidates)
	}
}

func TestRouterDeduplicatesRuleCandidates(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)
	msg := platform.Message{Channel: "feishu", ChatID: "c1", UserID: "u1"}

	router.extractRuleMemories(msg, "以后默认用中文回复", "")
	router.extractRuleMemories(msg, "以后默认用中文回复吧", "")

	candidates, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusCandidate})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("deduplicated candidates = %#v", candidates)
	}
	if len(candidates[0].Evidence) != 2 {
		t.Fatalf("merged evidence = %#v", candidates[0].Evidence)
	}

	if _, err := memories.Put(context.Background(), memory.Memory{
		Kind:       memory.KindProfile,
		Scope:      memory.ScopeChat,
		ScopeID:    "chat:feishu/c1",
		Content:    "以后默认用中文回答",
		Status:     memory.StatusActive,
		Confidence: 0.9,
	}); err != nil {
		t.Fatal(err)
	}
	router.extractRuleMemories(msg, "以后默认用中文回答。", "")
	candidates, err = memories.List(context.Background(), memory.ListOptions{Status: memory.StatusCandidate})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("active memory should suppress new candidates = %#v", candidates)
	}
}

func TestRouterAddsMemoryCheckHintOnlyForDurableTurns(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, _ := newTestRouterWithMemory(t, eng, plat)
	router.cfg.MemorySelfOrganize = true

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1",
		Text: "以后默认用中文回复",
	})
	waitForReplies(t, plat, 1)
	prompt, _ := eng.lastRequest()
	if !strings.Contains(prompt, "## Memory Check") {
		t.Fatalf("durable turn missing memory hint: %q", prompt)
	}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m2",
		Text: "hello",
	})
	waitForReplies(t, plat, 2)
	prompt, _ = eng.lastRequest()
	if strings.Contains(prompt, "## Memory Check") {
		t.Fatalf("ordinary turn should not get memory hint: %q", prompt)
	}
}

func TestRouterMemoryFeedbackCommand(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1",
		Text: "/remember 用户喜欢吃苹果",
	})
	waitForReplies(t, plat, 1)
	active, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil || len(active) != 1 {
		t.Fatalf("active = %#v err=%v", active, err)
	}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m2",
		Text: "/memory bad " + active[0].ID,
	})
	waitForReplies(t, plat, 2)
	updated, ok, err := memories.Get(context.Background(), active[0].ID)
	if err != nil || !ok {
		t.Fatalf("get feedback: ok=%v err=%v", ok, err)
	}
	if updated.HarmfulCount != 1 {
		t.Fatalf("harmful count = %d", updated.HarmfulCount)
	}
}

func TestRouterDoesNotRepeatUnchangedMemoryBlock(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1",
		Text: "/remember 以后默认用中文回复",
	})
	waitForReplies(t, plat, 1)
	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "first",
	})
	waitForReplies(t, plat, 2)
	firstPrompt, _ := eng.lastRequest()
	if !strings.Contains(firstPrompt, "Nectar Memory") {
		t.Fatalf("first prompt missing memory: %q", firstPrompt)
	}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m3", Text: "second",
	})
	waitForReplies(t, plat, 3)
	secondPrompt, _ := eng.lastRequest()
	if strings.Contains(secondPrompt, "Nectar Memory") {
		t.Fatalf("unchanged memory was injected twice: %q", secondPrompt)
	}

	if _, err := memories.Put(context.Background(), memory.Memory{
		Kind: memory.KindFact, Scope: memory.ScopeChat, ScopeID: memory.ChatScope("feishu", "c1"),
		Content: "新增的部署约定", Status: memory.StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m4", Text: "部署约定是什么",
	})
	waitForReplies(t, plat, 4)
	thirdPrompt, _ := eng.lastRequest()
	if !strings.Contains(thirdPrompt, "新增的部署约定") {
		t.Fatalf("changed memory was not injected: %q", thirdPrompt)
	}
}

func TestRouterGroupMemoryRequiresAdmin(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)
	router.cfg.AdminUsers = []string{"admin-1"}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "group-1", ChatType: "group", UserID: "guest-1",
		MessageID: "m1", Text: "/remember 群里默认用中文",
	})
	waitForReplies(t, plat, 1)
	pending, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusCandidate})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("group memory should be a candidate, got %#v", pending)
	}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "group-1", ChatType: "group", UserID: "guest-1",
		MessageID: "m2", Text: "/memory approve " + pending[0].ID,
	})
	waitForReplies(t, plat, 2)
	if got := plat.lastReply(); !strings.Contains(got, "admin users") {
		t.Fatalf("guest approval reply = %q", got)
	}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "group-1", ChatType: "group", UserID: "admin-1",
		MessageID: "m3", Text: "/memory approve " + pending[0].ID,
	})
	waitForReplies(t, plat, 3)
	active, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("admin approval did not activate memory: %#v", active)
	}
}

func TestIsTransientError(t *testing.T) {
	for _, text := range []string{"request timed out", "HTTP 429 rate limit", "connection reset", "context canceled"} {
		if !isTransientError(text) {
			t.Fatalf("%q should be transient", text)
		}
	}
	if isTransientError("compile failed: undefined: foo") {
		t.Fatal("compile error should not be transient")
	}
}

func TestRouterMemoryCandidateRequiresApproval(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{fail: true}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m1",
		Text:      "break",
	})
	waitForReplies(t, plat, 1)

	var pending []memory.Memory
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		pending, err = memories.List(context.Background(), memory.ListOptions{Status: memory.StatusCandidate})
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(pending) != 1 {
		t.Fatalf("pending memories = %d, want 1", len(pending))
	}

	eng.mu.Lock()
	eng.fail = false
	eng.mu.Unlock()
	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m2",
		Text:      "retry",
	})
	waitForReplies(t, plat, 2)
	prompt, _ := eng.lastRequest()
	if strings.Contains(prompt, "Nectar Memory") {
		t.Fatalf("candidate memory was injected before approval: %q", prompt)
	}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m3",
		Text:      "/memory approve " + pending[0].ID,
	})
	waitForReplies(t, plat, 3)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m4",
		Text:      "break again",
	})
	waitForReplies(t, plat, 4)
	prompt, _ = eng.lastRequest()
	if !strings.Contains(prompt, "Nectar Memory") || !strings.Contains(prompt, "Failure: boom") {
		t.Fatalf("approved memory was not injected: %q", prompt)
	}
}

func TestRouterGlobalMemoryRequiresAdmin(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _, memories := newTestRouterWithMemory(t, eng, plat)
	router.cfg.AdminUsers = []string{"admin-1"}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		UserID:    "guest-1",
		MessageID: "m1",
		Text:      "/remember global 全局偏好",
	})
	waitForReplies(t, plat, 1)
	if got := plat.lastReply(); !strings.Contains(got, "admin users") {
		t.Fatalf("guest reply = %q", got)
	}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		UserID:    "admin-1",
		MessageID: "m2",
		Text:      "/remember global 全局偏好",
	})
	waitForReplies(t, plat, 2)
	active, err := memories.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("active memories = %d, want 1", len(active))
	}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		UserID:    "guest-1",
		MessageID: "m3",
		Text:      "/memory forget " + active[0].ID,
	})
	waitForReplies(t, plat, 3)
	if got := plat.lastReply(); !strings.Contains(got, "admin users") {
		t.Fatalf("guest forget reply = %q", got)
	}
	if _, ok, err := memories.Get(context.Background(), active[0].ID); err != nil || !ok {
		t.Fatalf("global memory was deleted: ok=%v err=%v", ok, err)
	}
}
