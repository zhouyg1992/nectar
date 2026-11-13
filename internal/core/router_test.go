package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
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
	downloads    []attachment.Resource
	downloadDirs []string
	downloadErr  error
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
func (f *fakePlatform) Download(_ context.Context, resource attachment.Resource, dir string) (attachment.Local, error) {
	f.mu.Lock()
	f.downloads = append(f.downloads, resource)
	f.downloadDirs = append(f.downloadDirs, dir)
	index := len(f.downloads)
	err := f.downloadErr
	f.mu.Unlock()
	if err != nil {
		return attachment.Local{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return attachment.Local{}, err
	}
	path := filepath.Join(dir, fmt.Sprintf("attachment-%d", index))
	if err := os.WriteFile(path, []byte("test-data"), 0o600); err != nil {
		return attachment.Local{}, err
	}
	return attachment.Local{
		Kind:     resource.Kind,
		Path:     path,
		Name:     resource.Name,
		MimeType: "application/octet-stream",
	}, nil
}

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
	lastPrompt string
	lastOpts   engine.EngineOptions
	fail       bool
	resumeErr  error
}

type blockingEngine struct {
	started sync.Once
	startCh chan struct{}
	release chan struct{}
}

func (f *fakeEngine) Name() string { return "fake" }

func (f *fakeEngine) Start(_ context.Context, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	f.mu.Lock()
	f.starts++
	f.lastPrompt = prompt
	f.lastOpts = opts
	f.mu.Unlock()
	return f.events("session-start"), nil
}

func (f *fakeEngine) Resume(_ context.Context, sessionID, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	f.mu.Lock()
	f.resumes++
	f.lastResume = sessionID
	f.lastPrompt = prompt
	f.lastOpts = opts
	resumeErr := f.resumeErr
	f.mu.Unlock()
	if resumeErr != nil {
		return nil, resumeErr
	}
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

func (f *fakeEngine) lastRequest() (string, engine.EngineOptions) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPrompt, f.lastOpts
}

func (b *blockingEngine) Name() string { return "blocking" }

func (b *blockingEngine) Start(context.Context, string, engine.EngineOptions) (<-chan engine.Event, error) {
	ch := make(chan engine.Event)
	go func() {
		ch <- engine.Event{Type: engine.EventSessionStarted, SessionID: "blocking-session"}
		b.started.Do(func() { close(b.startCh) })
		<-b.release
		ch <- engine.Event{Type: engine.EventMessage, Text: "finished"}
		ch <- engine.Event{Type: engine.EventDone}
		close(ch)
	}()
	return ch, nil
}

func (b *blockingEngine) Resume(context.Context, string, string, engine.EngineOptions) (<-chan engine.Event, error) {
	return b.Start(context.Background(), "", engine.EngineOptions{})
}

func (b *blockingEngine) Cancel(context.Context, string) error { return nil }

type scriptedEngine struct {
	events []engine.Event
}

type fakeTypingPlatform struct {
	*fakePlatform
	started atomic.Int32
	stopped atomic.Int32
}

func (f *fakeTypingPlatform) StartTyping(context.Context, any) func() {
	f.started.Add(1)
	return func() { f.stopped.Add(1) }
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
	deadline := time.Now().Add(5 * time.Second)
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
	deadline := time.Now().Add(5 * time.Second)
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
	deadline := time.Now().Add(5 * time.Second)
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
	deadline := time.Now().Add(5 * time.Second)
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

func TestRouterExecuteScheduledWaitsForAgentCompletion(t *testing.T) {
	plat := &fakePlatform{}
	eng := &blockingEngine{
		startCh: make(chan struct{}),
		release: make(chan struct{}),
	}
	router, _ := newTestRouter(t, eng, plat)
	done := make(chan error, 1)
	go func() {
		done <- router.ExecuteScheduled(context.Background(), platform.Message{
			Channel: "feishu",
			ChatID:  "c1",
			Text:    "scheduled work",
			Origin:  "schedule",
		})
	}()

	<-eng.startCh
	select {
	case err := <-done:
		t.Fatalf("ExecuteScheduled returned before completion: %v", err)
	default:
	}
	close(eng.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ExecuteScheduled did not wait for completion")
	}
}

func TestRouterFallsBackWhenResumeFails(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &fakeEngine{resumeErr: errors.New("session not found")}
	router, _ := newTestRouter(t, eng, plat)
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	if err := router.sessions.Set(key, "stale-session"); err != nil {
		t.Fatal(err)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "hello"})
	waitForCardStatus(t, plat, platform.CardComplete)

	final := plat.lastUpdate()
	for _, want := range []string{"Previous session could not be resumed", "answer"} {
		if !strings.Contains(final.Body, want) {
			t.Fatalf("final body = %q, want %q", final.Body, want)
		}
	}
	if got, ok := router.sessions.Get(key); !ok || got != "session-start" {
		t.Fatalf("session = %q, %v, want session-start", got, ok)
	}
	starts, resumes, _ := eng.counts()
	if starts != 1 || resumes != 1 {
		t.Fatalf("starts=%d resumes=%d, want 1/1", starts, resumes)
	}
}

func TestRouterDownloadsAttachmentsAndPassesThemToEngine(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m-attachments",
		Text:      "看看这些附件",
		Attachments: []attachment.Resource{
			{Kind: attachment.KindImage, Key: "img_1"},
			{Kind: attachment.KindFile, Key: "file_1", Name: "report.pdf"},
		},
	})
	waitForReplies(t, plat, 1)

	prompt, opts := eng.lastRequest()
	if !strings.Contains(prompt, "看看这些附件") {
		t.Fatalf("prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "[Image 1 saved at: ") || !strings.Contains(prompt, `[File "report.pdf" saved at: `) {
		t.Fatalf("prompt missing attachment references: %q", prompt)
	}
	if len(opts.Attachments) != 2 {
		t.Fatalf("engine attachments = %#v", opts.Attachments)
	}
	if opts.Attachments[0].Kind != attachment.KindImage || opts.Attachments[1].Kind != attachment.KindFile {
		t.Fatalf("attachment kinds = %#v", opts.Attachments)
	}
	for _, item := range opts.Attachments {
		if !strings.Contains(item.Path, filepath.Join(".nectar", "attachments", "m-attachments")) {
			t.Fatalf("attachment path = %q", item.Path)
		}
	}
	if len(plat.downloadDirs) == 0 || plat.downloadDirs[0] == "" {
		t.Fatal("platform download directory was empty")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, err := os.Stat(opts.Attachments[0].Path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("attachment was not cleaned up: %q", opts.Attachments[0].Path)
}

func TestRouterAttachmentOnlyMessageUsesDefaultPrompt(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m-image",
		Attachments: []attachment.Resource{
			{Kind: attachment.KindImage, Key: "img_1"},
		},
	})
	waitForReplies(t, plat, 1)

	prompt, opts := eng.lastRequest()
	if !strings.Contains(prompt, "请分析附件中的图片。") {
		t.Fatalf("prompt = %q", prompt)
	}
	if len(opts.Attachments) != 1 || opts.Attachments[0].Kind != attachment.KindImage {
		t.Fatalf("attachments = %#v", opts.Attachments)
	}
}

func TestRouterReportsAttachmentDownloadFailure(t *testing.T) {
	plat := &fakePlatform{downloadErr: errors.New("boom")}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		MessageID: "m-fail",
		Text:      "看看图片",
		Attachments: []attachment.Resource{
			{Kind: attachment.KindImage, Key: "img_1"},
		},
	})
	waitForReplies(t, plat, 1)

	if got := plat.lastReply(); !strings.Contains(got, "Failed to download attachments") || !strings.Contains(got, "boom") {
		t.Fatalf("reply = %q", got)
	}
	if starts, resumes, _ := eng.counts(); starts != 0 || resumes != 0 {
		t.Fatalf("engine ran after download failure: starts=%d resumes=%d", starts, resumes)
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

func TestRouterStartsAndStopsTyping(t *testing.T) {
	base := &fakePlatform{}
	plat := &fakeTypingPlatform{fakePlatform: base}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "dingtalk", ChatID: "c1", MessageID: "m1", Text: "hello"})
	waitForReplies(t, base, 1)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && plat.stopped.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if plat.started.Load() != 1 || plat.stopped.Load() != 1 {
		t.Fatalf("typing started=%d stopped=%d", plat.started.Load(), plat.stopped.Load())
	}
}

func TestCardFooterIncludesContextUsage(t *testing.T) {
	footer := cardFooter("deepseek/deepseek-flash", 2*time.Second, &tokenUsage{
		InputTokens:   55200,
		OutputTokens:  100,
		ContextTokens: 55300,
	}, 258000)
	if !strings.Contains(footer, "ctx: 55.3k/258k (21.4%)") || !strings.Contains(footer, "deepseek-flash") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestCardFooterOmitsTurnTokens(t *testing.T) {
	footer := cardFooter("deepseek/deepseek-flash", 7600*time.Millisecond, &tokenUsage{
		InputTokens:       201,
		CachedInputTokens: 17280,
		OutputTokens:      16,
		ContextTokens:     17497,
	}, 996147)
	for _, want := range []string{"ctx: 17.5k/996.1k (1.8%)", "deepseek-flash", "7.6s"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer = %q, want %q", footer, want)
		}
	}
	for _, unwanted := range []string{"↑", "↓", "cached"} {
		if strings.Contains(footer, unwanted) {
			t.Fatalf("footer = %q, should not contain %q", footer, unwanted)
		}
	}
}

func TestFormatTurnUsage(t *testing.T) {
	got := formatTurnUsage(&tokenUsage{
		InputTokens:       201,
		CachedInputTokens: 17280,
		OutputTokens:      16,
	})
	if got != "↑201 (+17.3k cached) ↓16" {
		t.Fatalf("formatTurnUsage() = %q", got)
	}
}

func TestCardFooterOmitsContextWithoutLastCallUsage(t *testing.T) {
	footer := cardFooter("deepseek/deepseek-flash", time.Second, &tokenUsage{
		InputTokens:  55200,
		OutputTokens: 100,
	}, 258000)
	for _, unwanted := range []string{"ctx:", "↑", "↓"} {
		if strings.Contains(footer, unwanted) {
			t.Fatalf("footer = %q, should not contain %q", footer, unwanted)
		}
	}
	if !strings.Contains(footer, "deepseek-flash") || !strings.Contains(footer, "1.0s") {
		t.Fatalf("footer = %q", footer)
	}
}

func TestParseUsageIncludesCachedInput(t *testing.T) {
	usage := parseUsage(map[string]any{
		"usage": `{"input_tokens":201,"cached_input_tokens":17280,"output_tokens":16,"context_tokens":17497}`,
	})
	if usage == nil {
		t.Fatal("parseUsage() = nil")
	}
	if usage.InputTokens != 201 || usage.CachedInputTokens != 17280 ||
		usage.OutputTokens != 16 || usage.ContextTokens != 17497 {
		t.Fatalf("usage = %#v", usage)
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
		{"/new", platform.CardInfo, "🌱 New Session", "Started a new session"},
		{"/status", platform.CardInfo, "🌿 Session Status", "New session"},
		{"/stop", platform.CardInfo, "✋ Stop", "No agent is running"},
		{"", platform.CardError, "⚠️ Unsupported Message", "Only text, image, or file messages are supported"},
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

func TestRouterStatusShowsEngineModelAndSession(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	router.cfg.ModelName = "gpt-5.2-codex"
	if err := router.sessions.Set(SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}, "session-123"); err != nil {
		t.Fatal(err)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "/status"})
	waitForCardSends(t, plat, 1)

	plat.mu.Lock()
	body := plat.sentCards[0].Body
	plat.mu.Unlock()
	for _, want := range []string{"Engine: fake", "Model: gpt-5.2-codex", "Session: session-123"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status body = %q, want %q", body, want)
		}
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

func TestInitialCardBody(t *testing.T) {
	if got := initialCardBody(false); got != "Hold on, warming up the hive…" {
		t.Fatalf("initialCardBody(false) = %q", got)
	}
	if got := initialCardBody(true); got != "Gathering your attachments…" {
		t.Fatalf("initialCardBody(true) = %q", got)
	}
}

func TestRouterShowsToolCountAndReplacesIntermediateOutput(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &scriptedEngine{events: []engine.Event{
		{Type: engine.EventSessionStarted, SessionID: "s1"},
		{Type: engine.EventToolCall, Text: "ls"},
		{Type: engine.EventMessage, Text: "first intermediate output"},
		{Type: engine.EventToolCall, Text: "cat a.txt"},
		{Type: engine.EventMessage, Text: "final answer"},
		{Type: engine.EventDone},
	}}
	router, _ := newTestRouter(t, eng, plat)
	router.cfg.CardUpdateInterval = time.Hour

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "run"})
	waitForCardStatus(t, plat, platform.CardComplete)

	plat.mu.Lock()
	updates := append([]platform.Card(nil), plat.updates...)
	plat.mu.Unlock()

	foundOne := false
	foundTwo := false
	for _, card := range updates {
		if card.Activity == "⏳ Tool calls · 1" && card.Body == "first intermediate output" {
			foundOne = true
		}
		if card.Activity == "⏳ Tool calls · 2" && card.Body == "final answer" {
			foundTwo = true
		}
	}
	if !foundOne || !foundTwo {
		t.Fatalf("missing tool activity progress: %#v", updates)
	}

	final := plat.lastUpdate()
	if final.Activity != "" {
		t.Fatalf("final activity = %q, want empty", final.Activity)
	}
	if final.Body != "final answer" {
		t.Fatalf("final body = %q", final.Body)
	}
}

func TestRouterRendersEngineUsage(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &scriptedEngine{events: []engine.Event{
		{Type: engine.EventSessionStarted, SessionID: "s1"},
		{Type: engine.EventMessage, Text: "answer"},
		{Type: engine.EventDone, Meta: map[string]any{
			"usage": `{"input_tokens":120,"output_tokens":30,"context_tokens":180}`,
		}},
	}}
	router, _ := newTestRouter(t, eng, plat)
	router.cfg.ContextWindow = 1000

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "hello"})
	waitForCardStatus(t, plat, platform.CardComplete)

	final := plat.lastUpdate()
	for _, want := range []string{"ctx: 180/1k (18.0%)"} {
		if !strings.Contains(final.Footer, want) {
			t.Fatalf("footer = %q, want %q", final.Footer, want)
		}
	}
	for _, unwanted := range []string{"↑", "↓"} {
		if strings.Contains(final.Footer, unwanted) {
			t.Fatalf("footer = %q, should not contain %q", final.Footer, unwanted)
		}
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/status"})
	waitForCardSends(t, plat, 2)
	plat.mu.Lock()
	statusBody := plat.sentCards[1].Body
	plat.mu.Unlock()
	for _, want := range []string{"Context: 180/1k (18.0%)", "Last turn: ↑120 ↓30"} {
		if !strings.Contains(statusBody, want) {
			t.Fatalf("status body = %q, want %q", statusBody, want)
		}
	}
}

func TestRouterUsesEngineContextWindow(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &scriptedEngine{events: []engine.Event{
		{Type: engine.EventSessionStarted, SessionID: "s1"},
		{Type: engine.EventMessage, Text: "answer"},
		{Type: engine.EventDone, Meta: map[string]any{
			"usage":          `{"input_tokens":120,"output_tokens":30,"context_tokens":180}`,
			"context_window": 1_000_000,
		}},
	}}
	router, _ := newTestRouter(t, eng, plat)
	router.cfg.ContextWindow = 258000

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "hello"})
	waitForCardStatus(t, plat, platform.CardComplete)

	final := plat.lastUpdate()
	if !strings.Contains(final.Footer, "ctx: 180/1M (0.0%)") {
		t.Fatalf("footer = %q", final.Footer)
	}
}

func TestRouterRecoverableToolErrorStillCompletes(t *testing.T) {
	plat := &fakePlatform{cards: true}
	eng := &scriptedEngine{events: []engine.Event{
		{Type: engine.EventSessionStarted, SessionID: "s1"},
		{Type: engine.EventToolResult, Text: "read failed", Meta: map[string]any{"status": "error"}},
		{Type: engine.EventMessage, Text: "Final answer"},
		{Type: engine.EventDone},
	}}
	router, _ := newTestRouter(t, eng, plat)
	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "hello"})
	waitForCardStatus(t, plat, platform.CardComplete)

	final := plat.lastUpdate()
	if final.Status != platform.CardComplete || final.Body != "Final answer" {
		t.Fatalf("final card = %#v", final)
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

func TestRouterInjectsNectarSessionEnv(t *testing.T) {
	eng := &fakeEngine{}
	plat := &fakePlatform{}
	router, _ := newTestRouter(t, eng, plat)
	router.cfg.DataDir = "/tmp/nectar-data"
	router.cfg.APISocket = "/tmp/nectar-data/run/api.sock"

	router.Handle(platform.Message{
		Channel:  "feishu",
		ChatID:   "chat-1",
		ChatType: "group",
		UserID:   "user-1",
		ThreadID: "thread-1",
		Text:     "hello",
	})
	waitForReplies(t, plat, 1)
	_, opts := eng.lastRequest()
	if opts.Env["NECTAR_CHAT_ID"] != "chat-1" ||
		opts.Env["NECTAR_THREAD_ID"] != "thread-1" ||
		opts.Env["NECTAR_USER_ID"] != "user-1" ||
		opts.Env["NECTAR_API_SOCKET"] != "/tmp/nectar-data/run/api.sock" ||
		opts.Env["NECTAR_CLI"] == "" {
		t.Fatalf("env = %#v", opts.Env)
	}
}
