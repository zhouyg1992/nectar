package core

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/core/decision"
	"github.com/zhouyg1992/nectar/internal/engine"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/memory/comb"
	"github.com/zhouyg1992/nectar/internal/platform"
)

type fakePlatform struct {
	mu           sync.Mutex
	cards        bool
	replies      []string
	sentCards    []platform.Card
	updates      []platform.Card
	replyTargets []string
	replyThreads []bool
	threadIDs    []string
	updateIDs    []string
}

func (f *fakePlatform) Name() string { return "fake" }
func (f *fakePlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{Card: f.cards, Streaming: f.cards}
}
func (f *fakePlatform) Start(context.Context, func(platform.Message)) error {
	return nil
}
func (f *fakePlatform) Send(_ context.Context, reply platform.Reply) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyTargets = append(f.replyTargets, reply.ReplyTo)
	f.replyThreads = append(f.replyThreads, reply.ReplyInThread)
	f.threadIDs = append(f.threadIDs, reply.ThreadID)
	if reply.Card != nil {
		f.sentCards = append(f.sentCards, *reply.Card)
		return "card-1", nil
	}
	f.replies = append(f.replies, reply.Text)
	return "reply", nil
}
func (f *fakePlatform) Update(_ context.Context, messageID string, reply platform.Reply) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyTargets = append(f.replyTargets, reply.ReplyTo)
	f.replyThreads = append(f.replyThreads, reply.ReplyInThread)
	f.threadIDs = append(f.threadIDs, reply.ThreadID)
	f.updateIDs = append(f.updateIDs, messageID)
	if reply.Card != nil {
		f.updates = append(f.updates, *reply.Card)
	}
	return nil
}
func (f *fakePlatform) Download(context.Context, string) (string, error) { return "", nil }

func (f *fakePlatform) replyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.replies)
}

func (f *fakePlatform) lastReply() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.replies) == 0 {
		return ""
	}
	return f.replies[len(f.replies)-1]
}

func (f *fakePlatform) cardUpdateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.updates)
}

func (f *fakePlatform) cardSendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sentCards)
}

func (f *fakePlatform) lastUpdate() platform.Card {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updates) == 0 {
		return platform.Card{}
	}
	return f.updates[len(f.updates)-1]
}

func (f *fakePlatform) usesQuotedReplies() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, target := range f.replyTargets {
		if target != "" {
			return true
		}
	}
	return false
}

type fakeEngine struct {
	mu         sync.Mutex
	starts     int
	resumes    int
	lastResume string
	fail       bool
}

func (f *fakeEngine) Name() string { return "fake" }

func (f *fakeEngine) Start(context.Context, string, engine.EngineOptions) (<-chan engine.Event, error) {
	f.mu.Lock()
	f.starts++
	f.mu.Unlock()
	return f.events("session-start"), nil
}

func (f *fakeEngine) Resume(_ context.Context, sessionID, _ string, _ engine.EngineOptions) (<-chan engine.Event, error) {
	f.mu.Lock()
	f.resumes++
	f.lastResume = sessionID
	f.mu.Unlock()
	return f.events("session-resume"), nil
}

func (f *fakeEngine) Cancel(context.Context, string) error { return nil }

func (f *fakeEngine) events(sessionID string) <-chan engine.Event {
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()

	ch := make(chan engine.Event, 4)
	if fail {
		ch <- engine.Event{Type: engine.EventError, Text: "boom"}
		close(ch)
		return ch
	}
	ch <- engine.Event{Type: engine.EventSessionStarted, SessionID: sessionID}
	ch <- engine.Event{Type: engine.EventMessage, Text: "answer"}
	ch <- engine.Event{Type: engine.EventDone}
	close(ch)
	return ch
}

func (f *fakeEngine) counts() (int, int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.resumes, f.lastResume
}

type scriptedEngine struct {
	events []engine.Event
}

func (s *scriptedEngine) Name() string { return "scripted" }

func (s *scriptedEngine) Start(context.Context, string, engine.EngineOptions) (<-chan engine.Event, error) {
	return s.stream(), nil
}

func (s *scriptedEngine) Resume(context.Context, string, string, engine.EngineOptions) (<-chan engine.Event, error) {
	return s.stream(), nil
}

func (s *scriptedEngine) Cancel(context.Context, string) error { return nil }

func (s *scriptedEngine) stream() <-chan engine.Event {
	ch := make(chan engine.Event, len(s.events))
	for _, event := range s.events {
		ch <- event
	}
	close(ch)
	return ch
}

func newTestRouter(t *testing.T, eng engine.EngineAdapter, plat platform.PlatformAdapter) (*Router, *comb.FileStore) {
	t.Helper()
	ws, err := execguard.NewWorkspace(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	lessons, err := comb.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(
		context.Background(),
		plat,
		eng,
		NewSessionStore(),
		NewQueue(),
		ws,
		lessons,
		decision.Noop{},
		logger,
		RouterConfig{AgentID: "codex"},
	)
	return router, lessons
}

func waitForReplies(t *testing.T, plat *fakePlatform, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if plat.replyCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("got %d replies, want %d", plat.replyCount(), want)
}

func waitForCardUpdates(t *testing.T, plat *fakePlatform, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if plat.cardUpdateCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("got %d card updates, want %d", plat.cardUpdateCount(), want)
}

func waitForCardSends(t *testing.T, plat *fakePlatform, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if plat.cardSendCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("got %d card sends, want %d", plat.cardSendCount(), want)
}

func waitForCardStatus(t *testing.T, plat *fakePlatform, status platform.CardStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if plat.cardUpdateCount() > 0 && plat.lastUpdate().Status == status {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("card did not reach status %q", status)
}

func TestRouterStartsThenResumes(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	msg := platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "hello"}

	router.Handle(msg)
	waitForReplies(t, plat, 1)
	if got := plat.lastReply(); got != "answer" {
		t.Fatalf("first reply = %q", got)
	}

	msg.MessageID = "m2"
	msg.Text = "again"
	router.Handle(msg)
	waitForReplies(t, plat, 2)

	starts, resumes, last := eng.counts()
	if starts != 1 || resumes != 1 || last != "session-start" {
		t.Fatalf("starts=%d resumes=%d last=%q", starts, resumes, last)
	}

	msg.MessageID = "m3"
	msg.Text = "/new"
	router.Handle(msg)
	waitForReplies(t, plat, 3)

	msg.MessageID = "m4"
	msg.Text = "fresh"
	router.Handle(msg)
	waitForReplies(t, plat, 4)

	starts, resumes, _ = eng.counts()
	if starts != 2 || resumes != 1 {
		t.Fatalf("after /new starts=%d resumes=%d", starts, resumes)
	}
}

func TestRouterRecordsLessonOnError(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{fail: true}
	router, lessons := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "break"})
	waitForReplies(t, plat, 1)

	if got := plat.lastReply(); got == "" {
		t.Fatal("empty error reply")
	}
	pending, err := lessons.ListPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending lessons = %d, want 1", len(pending))
	}
}

func TestRouterSendsThinkingCardAndUpdatesFinal(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "hello"})
	waitForCardUpdates(t, plat, 1)

	plat.mu.Lock()
	if len(plat.sentCards) != 1 {
		plat.mu.Unlock()
		t.Fatalf("sent cards = %d, want 1", len(plat.sentCards))
	}
	initial := plat.sentCards[0]
	plat.mu.Unlock()
	if initial.Status != platform.CardThinking || initial.Title != cardTitleThinking {
		t.Fatalf("initial card = %#v", initial)
	}

	final := plat.lastUpdate()
	if final.Status != platform.CardComplete || final.Title != cardTitleComplete || final.Body != "answer" {
		t.Fatalf("final card = %#v", final)
	}
	if plat.usesQuotedReplies() {
		t.Fatal("router quoted the user's message")
	}
}

func TestCardFooterIncludesContextUsage(t *testing.T) {
	footer := cardFooter("deepseek/deepseek-flash", 2*time.Second, &tokenUsage{
		InputTokens:  55200,
		OutputTokens: 100,
	}, 258000)
	if !strings.Contains(footer, "ctx: 55.3k/258k (21.4%)") || !strings.Contains(footer, "deepseek-flash") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestDisplayModelName(t *testing.T) {
	if got := displayModelName("deepseek/deepseek-flash"); got != "deepseek-flash" {
		t.Fatalf("displayModelName() = %q", got)
	}
	if got := displayModelName("gpt-5.2-codex"); got != "gpt-5.2-codex" {
		t.Fatalf("displayModelName() = %q", got)
	}
}

func TestRouterHelpCardBypassesEngine(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "/help"})
	waitForCardSends(t, plat, 1)

	plat.mu.Lock()
	card := plat.sentCards[0]
	plat.mu.Unlock()
	if card.Status != platform.CardInfo || card.Title != "🍯 Nectar Commands" {
		t.Fatalf("help card = %#v", card)
	}
	if !strings.Contains(card.Body, "/new") || !strings.Contains(card.Body, "/commands") {
		t.Fatalf("help card body = %q", card.Body)
	}
	if starts, resumes, _ := eng.counts(); starts != 0 || resumes != 0 {
		t.Fatalf("help invoked engine: starts=%d resumes=%d", starts, resumes)
	}
}

func TestRouterNaturalLanguageHelpBypassesEngine(t *testing.T) {
	for _, text := range []string{
		"nectar 有哪些命令",
		"有哪些可用命令",
		"what commands are available",
	} {
		t.Run(text, func(t *testing.T) {
			plat := &fakePlatform{cards: true}
			eng := &fakeEngine{}
			router, _ := newTestRouter(t, eng, plat)
			router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: text})
			waitForCardSends(t, plat, 1)
			if starts, resumes, _ := eng.counts(); starts != 0 || resumes != 0 {
				t.Fatalf("help invoked engine: starts=%d resumes=%d", starts, resumes)
			}
		})
	}
}

func TestRouterHelpFallsBackToText(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "/help"})
	waitForReplies(t, plat, 1)
	if got := plat.lastReply(); !strings.Contains(got, "/new") {
		t.Fatalf("help text = %q", got)
	}
	if starts, resumes, _ := eng.counts(); starts != 0 || resumes != 0 {
		t.Fatalf("help invoked engine: starts=%d resumes=%d", starts, resumes)
	}
}

func TestRouterCommandResultsUseCards(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)

	cases := []struct {
		text     string
		status   platform.CardStatus
		title    string
		bodyPart string
	}{
		{"/new", platform.CardInfo, "🔄 New Session", "已开始新会话"},
		{"/status", platform.CardInfo, "📊 Session Status", "新会话"},
		{"/stop", platform.CardInfo, "⏹ Stop", "还没有正在运行的 Agent"},
		{"", platform.CardError, "⚠️ Unsupported Message", "只支持文本消息"},
	}

	for i, tc := range cases {
		router.Handle(platform.Message{
			Channel:   "feishu",
			ChatID:    "c1",
			MessageID: "m1",
			Text:      tc.text,
		})
		waitForCardSends(t, plat, i+1)
		plat.mu.Lock()
		card := plat.sentCards[i]
		plat.mu.Unlock()
		if card.Status != tc.status || card.Title != tc.title || !strings.Contains(card.Body, tc.bodyPart) {
			t.Fatalf("command %q card = %#v", tc.text, card)
		}
	}

	if starts, resumes, _ := eng.counts(); starts != 0 || resumes != 0 {
		t.Fatalf("commands invoked engine: starts=%d resumes=%d", starts, resumes)
	}
}

func TestRouterThreadRepliesStayInThread(t *testing.T) {
	t.Run("help card", func(t *testing.T) {
		plat := &fakePlatform{cards: true}
		eng := &fakeEngine{}
		router, _ := newTestRouter(t, eng, plat)
		router.Handle(platform.Message{
			Channel:   "feishu",
			ChatID:    "c1",
			MessageID: "m1",
			ThreadID:  "th1",
			Text:      "/help",
		})
		waitForCardSends(t, plat, 1)
		plat.mu.Lock()
		replyTo := plat.replyTargets[0]
		inThread := plat.replyThreads[0]
		threadID := plat.threadIDs[0]
		plat.mu.Unlock()
		if replyTo != "m1" || !inThread || threadID != "th1" {
			t.Fatalf("thread reply = to:%q inThread:%v thread:%q", replyTo, inThread, threadID)
		}
	})

	t.Run("agent card", func(t *testing.T) {
		plat := &fakePlatform{cards: true}
		eng := &fakeEngine{}
		router, _ := newTestRouter(t, eng, plat)
		router.Handle(platform.Message{
			Channel:   "feishu",
			ChatID:    "c1",
			MessageID: "m1",
			ThreadID:  "th1",
			Text:      "hello",
		})
		waitForCardStatus(t, plat, platform.CardComplete)
		plat.mu.Lock()
		replyTo := plat.replyTargets[0]
		inThread := plat.replyThreads[0]
		threadID := plat.threadIDs[0]
		plat.mu.Unlock()
		if replyTo != "m1" || !inThread || threadID != "th1" {
			t.Fatalf("thread reply = to:%q inThread:%v thread:%q", replyTo, inThread, threadID)
		}
	})

	t.Run("text fallback", func(t *testing.T) {
		plat := &fakePlatform{}
		eng := &fakeEngine{}
		router, _ := newTestRouter(t, eng, plat)
		router.Handle(platform.Message{
			Channel:   "feishu",
			ChatID:    "c1",
			MessageID: "m1",
			ThreadID:  "th1",
			Text:      "/help",
		})
		waitForReplies(t, plat, 1)
		plat.mu.Lock()
		replyTo := plat.replyTargets[0]
		inThread := plat.replyThreads[0]
		threadID := plat.threadIDs[0]
		plat.mu.Unlock()
		if replyTo != "m1" || !inThread || threadID != "th1" {
			t.Fatalf("thread reply = to:%q inThread:%v thread:%q", replyTo, inThread, threadID)
		}
	})
}

func TestReplyTarget(t *testing.T) {
	normal := replyTarget(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1"})
	if normal.ReplyTo != "" || normal.ReplyInThread || normal.ThreadID != "" {
		t.Fatalf("normal reply target = %#v", normal)
	}
	threaded := replyTarget(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1", ThreadID: "th1",
	})
	if threaded.ReplyTo != "m1" || !threaded.ReplyInThread || threaded.ThreadID != "th1" {
		t.Fatalf("threaded reply target = %#v", threaded)
	}
}

func TestRouterIsolatesTopicSessions(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "main"})
	waitForReplies(t, plat, 1)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", ThreadID: "th1", Text: "topic one"})
	waitForReplies(t, plat, 2)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m3", ThreadID: "th1", Text: "topic two"})
	waitForReplies(t, plat, 3)

	starts, resumes, _ := eng.counts()
	if starts != 2 || resumes != 1 {
		t.Fatalf("topic session isolation: starts=%d resumes=%d, want 2/1", starts, resumes)
	}
}

func TestRouterStreamsProgressAndFinalOnly(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &scriptedEngine{events: []engine.Event{
		{Type: engine.EventSessionStarted, SessionID: "s1"},
		{Type: engine.EventMessage, Text: "Checking files..."},
		{Type: engine.EventToolCall, Text: "rg foo"},
		{Type: engine.EventMessage, Text: "Reading router..."},
		{Type: engine.EventMessage, Text: "Final answer only"},
		{Type: engine.EventDone},
	}}
	router, _ := newTestRouter(t, eng, plat)
	router.cfg.CardUpdateInterval = time.Nanosecond

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "analyze"})
	waitForCardStatus(t, plat, platform.CardComplete)

	final := plat.lastUpdate()
	if final.Body != "Final answer only" {
		t.Fatalf("final card body = %q", final.Body)
	}

	plat.mu.Lock()
	defer plat.mu.Unlock()
	foundProgress := false
	for _, card := range plat.updates {
		if strings.Contains(card.Body, "Checking files") || strings.Contains(card.Body, "Reading router") {
			foundProgress = true
			break
		}
	}
	if !foundProgress {
		t.Fatalf("intermediate output was not streamed: %#v", plat.updates)
	}
}

func TestIsHelpRequest(t *testing.T) {
	positive := []string{
		"/help", "/commands", "help", "命令",
		"nectar 有哪些命令", "有哪些可用命令",
		"what commands are available", "what can you do", "show commands",
	}
	for _, text := range positive {
		if !isHelpRequest(text) {
			t.Fatalf("expected help intent: %q", text)
		}
	}
	negative := []string{
		"help me fix this bug",
		"写一个命令解析器",
		"这个命令怎么用",
		"请实现 /help 命令",
		"what commands does this shell support",
	}
	for _, text := range negative {
		if isHelpRequest(text) {
			t.Fatalf("unexpected help intent: %q", text)
		}
	}
}
