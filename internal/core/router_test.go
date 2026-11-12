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
