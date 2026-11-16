package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/platform"
)

type steerableBlockingEngine struct {
	started   chan struct{}
	release   chan struct{}
	steers    chan string
	steerErr  error
	startOnce sync.Once
}

func newSteerableBlockingEngine() *steerableBlockingEngine {
	return &steerableBlockingEngine{
		started: make(chan struct{}),
		release: make(chan struct{}),
		steers:  make(chan string, 8),
	}
}

func (e *steerableBlockingEngine) Name() string { return "steerable" }

func (e *steerableBlockingEngine) Start(context.Context, string, engine.EngineOptions) (<-chan engine.Event, error) {
	return e.stream(), nil
}

func (e *steerableBlockingEngine) Resume(context.Context, string, string, engine.EngineOptions) (<-chan engine.Event, error) {
	return e.stream(), nil
}

func (e *steerableBlockingEngine) Cancel(context.Context, string) error { return nil }

func (e *steerableBlockingEngine) Steer(_ context.Context, _ string, text string) error {
	if e.steerErr != nil {
		return e.steerErr
	}
	e.steers <- text
	return nil
}

func (e *steerableBlockingEngine) stream() <-chan engine.Event {
	ch := make(chan engine.Event, 3)
	go func() {
		ch <- engine.Event{Type: engine.EventSessionStarted, SessionID: "steer-session"}
		e.startOnce.Do(func() { close(e.started) })
		<-e.release
		ch <- engine.Event{Type: engine.EventMessage, Text: "answer"}
		ch <- engine.Event{Type: engine.EventDone}
		close(ch)
	}()
	return ch
}

func waitForEngineSession(t *testing.T, router *Router, key SessionKey, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if session, ok := router.sessions.Active(key); ok && session.EngineSessionID == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("engine session was not persisted")
}

func waitForReplyContaining(t *testing.T, plat *fakePlatform, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		plat.mu.Lock()
		for _, reply := range plat.replies {
			if strings.Contains(reply, want) {
				plat.mu.Unlock()
				return
			}
		}
		plat.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	plat.mu.Lock()
	replies := append([]string(nil), plat.replies...)
	plat.mu.Unlock()
	t.Fatalf("no reply containing %q; replies=%v", want, replies)
}

func TestSteerCommandAliasesInjectIntoRunningTask(t *testing.T) {
	plat := &fakePlatform{}
	eng := newSteerableBlockingEngine()
	router, _ := newTestRouter(t, eng, plat)
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "run task"})
	<-eng.started
	waitForEngineSession(t, router, key, "steer-session")

	for i, command := range []string{"/append first", "/steer second", "/ps third", "/btw fourth"} {
		router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "s" + string(rune('1'+i)), Text: command})
		want := []string{"first", "second", "third", "fourth"}[i]
		select {
		case got := <-eng.steers:
			if got != want {
				t.Fatalf("steered text = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("steer %q was not delivered", want)
		}
		waitForReplies(t, plat, i+1)
	}
	close(eng.release)
	waitForReplyContaining(t, plat, "answer")
}

func TestSteerCommandDoesNotQueueOrdinaryMessages(t *testing.T) {
	plat := &fakePlatform{}
	eng := newSteerableBlockingEngine()
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "run task"})
	<-eng.started
	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "ordinary"})

	select {
	case got := <-eng.steers:
		t.Fatalf("ordinary message unexpectedly steered: %q", got)
	case <-time.After(150 * time.Millisecond):
	}
	close(eng.release)
	waitForReplyContaining(t, plat, "answer")
}

func TestSteerCommandReportsUnsupportedEngine(t *testing.T) {
	plat := &fakePlatform{}
	eng := &blockingEngine{startCh: make(chan struct{}), release: make(chan struct{})}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "run task"})
	<-eng.startCh
	waitForEngineSession(t, router, SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}, "blocking-session")
	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/steer change"})
	waitForReplyContaining(t, plat, "does not support steering")
	close(eng.release)
}

func TestSteerCommandReportsTurnEnded(t *testing.T) {
	plat := &fakePlatform{}
	eng := newSteerableBlockingEngine()
	eng.steerErr = engine.ErrNoActiveTurn
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "run task"})
	<-eng.started
	waitForEngineSession(t, router, SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}, "steer-session")
	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/ps too late"})
	waitForReplyContaining(t, plat, "ended before")
	close(eng.release)
}

func TestSteerCommandNoRunningTaskDoesNotCallEngine(t *testing.T) {
	plat := &fakePlatform{}
	eng := newSteerableBlockingEngine()
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "/steer nothing running"})
	waitForReplyContaining(t, plat, "no running task")
	select {
	case got := <-eng.steers:
		t.Fatalf("unexpected steer: %q", got)
	default:
	}
}

func TestSteerCommandReportsEmptyUsage(t *testing.T) {
	plat := &fakePlatform{}
	eng := newSteerableBlockingEngine()
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "/btw"})
	waitForReplyContaining(t, plat, "Usage: /steer")
}

func TestSteerCommandReportsGenericFailure(t *testing.T) {
	plat := &fakePlatform{}
	eng := newSteerableBlockingEngine()
	eng.steerErr = errors.New("transport down")
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "run task"})
	<-eng.started
	waitForEngineSession(t, router, SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}, "steer-session")
	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/append change"})
	waitForReplyContaining(t, plat, "Could not insert")
	close(eng.release)
}
