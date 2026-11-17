package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/core/decision"
	"github.com/zhouyg1992/nectar/internal/engine"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/platform"
)

type relayTestEngine struct {
	name string

	mu         sync.Mutex
	starts     int
	resumes    int
	lastPrompt string
	lastOpts   engine.EngineOptions
	lastResume string
	onStart    func(prompt string, opts engine.EngineOptions) (string, error)
	onResume   func(sessionID, prompt string, opts engine.EngineOptions) (string, error)
}

func (e *relayTestEngine) Name() string { return e.name }

func (e *relayTestEngine) Start(_ context.Context, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	e.mu.Lock()
	e.starts++
	e.lastPrompt = prompt
	e.lastOpts = opts
	onStart := e.onStart
	e.mu.Unlock()
	text := ""
	var err error
	if onStart != nil {
		text, err = onStart(prompt, opts)
	}
	if err != nil {
		return nil, err
	}
	return relayEvents("relay-start", text), nil
}

func (e *relayTestEngine) Resume(_ context.Context, sessionID, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	e.mu.Lock()
	e.resumes++
	e.lastPrompt = prompt
	e.lastOpts = opts
	e.lastResume = sessionID
	onResume := e.onResume
	e.mu.Unlock()
	text := ""
	var err error
	if onResume != nil {
		text, err = onResume(sessionID, prompt, opts)
	}
	if err != nil {
		return nil, err
	}
	return relayEvents("relay-resume", text), nil
}

func (e *relayTestEngine) Cancel(context.Context, string) error { return nil }

func (e *relayTestEngine) counts() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts, e.resumes
}

func (e *relayTestEngine) request() (string, engine.EngineOptions) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastPrompt, e.lastOpts
}

func relayEvents(sessionID, text string) <-chan engine.Event {
	ch := make(chan engine.Event, 3)
	ch <- engine.Event{Type: engine.EventSessionStarted, SessionID: sessionID}
	ch <- engine.Event{Type: engine.EventMessage, Text: text}
	ch <- engine.Event{Type: engine.EventDone}
	close(ch)
	return ch
}

func waitForReplyContains(t *testing.T, plat *fakePlatform, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, reply := range plat.repliesSnapshot() {
			if strings.Contains(reply, want) {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("replies %#v do not contain %q", plat.repliesSnapshot(), want)
}

func newRelayTestRouter(t *testing.T, eng engine.EngineAdapter, plat platform.PlatformAdapter, botID, agentID string, ws *execguard.Workspace, locks *execguard.WorkspaceLocks, hub *RelayHub) *Router {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(
		context.Background(),
		plat,
		eng,
		NewSessionStore(),
		NewQueue(),
		ws,
		nil,
		nil,
		decision.Noop{},
		logger,
		RouterConfig{
			BotID:              botID,
			AgentID:            agentID,
			BotName:            botID,
			Relay:              hub,
			WorkspaceLocks:     locks,
			MemoryDefaultScope: "chat",
		},
	)
}

func newRelayTestHub(t *testing.T, echo string, maxGen int) *RelayHub {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub, err := NewRelayHub(RelayOptions{
		Enabled:             true,
		Echo:                echo,
		MaxGeneration:       maxGen,
		MaxEnvelopesPerRoot: 12,
		MaxRunsPerRoot:      8,
		RootTTL:             30 * time.Minute,
		DataDir:             t.TempDir(),
		Logger:              logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return hub
}

func TestRelayAskReplyReturnsToOriginSession(t *testing.T) {
	hub := newRelayTestHub(t, "summary", 4)
	defer hub.Close()
	wsDir := t.TempDir()
	locks := execguard.NewWorkspaceLocks()
	wsSource, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wsTarget, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	sourcePlat := &fakePlatform{}
	targetPlat := &fakePlatform{}
	sourceEngine := &relayTestEngine{
		name: "codex",
		onStart: func(_ string, opts engine.EngineOptions) (string, error) {
			token := opts.Env["NECTAR_RELAY_TOKEN"]
			if token == "" {
				return "", errors.New("missing relay token")
			}
			_, _, err := hub.Send(context.Background(), token, RelaySendRequest{
				To:   "reviewer",
				Body: "please review this diff",
			})
			return "asked reviewer", err
		},
		onResume: func(_ string, _ string, _ engine.EngineOptions) (string, error) {
			return "applied review", nil
		},
	}
	targetEngine := &relayTestEngine{
		name: "claude",
		onStart: func(_ string, _ engine.EngineOptions) (string, error) {
			return "looks good", nil
		},
	}
	source := newRelayTestRouter(t, sourceEngine, sourcePlat, "coder", "codex", wsSource, locks, hub)
	target := newRelayTestRouter(t, targetEngine, targetPlat, "reviewer", "claude", wsTarget, locks, hub)
	hub.Register(RelayBotConfig{
		ID:         "coder",
		Name:       "Coder",
		AcceptFrom: []string{"reviewer"},
		PromptMode: "full",
		Router:     source,
	})
	hub.Register(RelayBotConfig{
		ID:         "reviewer",
		Name:       "Reviewer",
		AcceptFrom: []string{"coder"},
		PromptMode: "minimal",
		Router:     target,
	})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	source.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		ChatType:  "group",
		MessageID: "m1",
		UserID:    "u1",
		Text:      "implement X",
	})
	waitForReplyContains(t, sourcePlat, "applied review")
	waitForReplyContains(t, targetPlat, "looks good")

	if got := sourcePlat.lastReply(); got != "applied review" {
		t.Fatalf("source last reply = %q, want applied review", got)
	}
	if got := targetPlat.lastReply(); got != "looks good" {
		t.Fatalf("target last reply = %q, want looks good", got)
	}
	prompt, opts := targetEngine.request()
	if !strings.Contains(prompt, "please review this diff") {
		t.Fatalf("target prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "<relay") || !strings.Contains(prompt, `from="coder"`) {
		t.Fatalf("target prompt missing relay fence: %q", prompt)
	}
	if !strings.Contains(prompt, "<relay_rules>") || !strings.Contains(prompt, "do not poll") {
		t.Fatalf("target prompt missing async relay rules: %q", prompt)
	}
	if !strings.Contains(prompt, `id="coder"`) || !strings.Contains(prompt, `name="Coder"`) {
		t.Fatalf("target prompt missing peer display name: %q", prompt)
	}
	if opts.Env["NECTAR_RELAY_TOKEN"] == "" || opts.Env["NECTAR_RELAY_ROOT"] == "" {
		t.Fatalf("target relay env = %#v", opts.Env)
	}
	if starts, resumes := sourceEngine.counts(); starts != 1 || resumes != 1 {
		t.Fatalf("source engine starts/resumes = %d/%d, want 1/1", starts, resumes)
	}
	sourceKey := SessionKey{BotID: "coder", Channel: "feishu", ChatID: "c1", AgentID: "codex"}.normalized()
	if items := source.sessions.List(sourceKey, false); len(items) != 1 {
		t.Fatalf("source sessions = %#v, want only the origin session", items)
	}
	targetKey := SessionKey{BotID: "reviewer", Channel: "feishu", ChatID: "c1", AgentID: "claude"}.normalized()
	targetSessions := target.sessions.List(targetKey, false)
	if len(targetSessions) != 1 || !strings.HasPrefix(targetSessions[0].Name, "relay:") {
		t.Fatalf("target sessions = %#v", targetSessions)
	}

	roots := hub.Roots()
	if len(roots) != 1 {
		t.Fatalf("roots = %#v", roots)
	}
	_, envelopes, ok := hub.Root(roots[0].ID)
	if !ok || len(envelopes) != 2 {
		t.Fatalf("envelopes = %#v, ok = %v", envelopes, ok)
	}
	if envelopes[0].Gen != 1 || envelopes[1].Gen != 2 {
		t.Fatalf("generations = %d, %d", envelopes[0].Gen, envelopes[1].Gen)
	}
	seenMirror := false
	for _, card := range sourcePlat.sentCardsSnapshot() {
		if card.Status == platform.CardInfo && strings.Contains(card.Title, "任务已转交") {
			seenMirror = true
			if !strings.Contains(card.Body, "任务：please review this diff") {
				t.Fatalf("mirror card body = %q", card.Body)
			}
			if !strings.Contains(card.Body, relayMarkerPrefix) {
				t.Fatalf("mirror card missing invisible marker: %q", card.Body)
			}
			if strings.Contains(card.Body, "⟦nectar-relay:") {
				t.Fatalf("mirror card leaks legacy marker: %q", card.Body)
			}
			break
		}
	}
	if !seenMirror {
		t.Fatalf("source cards missing relay handoff: %#v", sourcePlat.sentCardsSnapshot())
	}
}

func TestRelayMirrorMessageIsIgnored(t *testing.T) {
	hub := newRelayTestHub(t, "summary", 4)
	defer hub.Close()
	wsDir := t.TempDir()
	ws, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	plat := &fakePlatform{}
	eng := &relayTestEngine{name: "reviewer"}
	router := newRelayTestRouter(t, eng, plat, "reviewer", "claude", ws, execguard.NewWorkspaceLocks(), hub)
	hub.Register(RelayBotConfig{ID: "reviewer", Name: "Reviewer", Router: router})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		ChatType:  "group",
		MessageID: "m1",
		UserID:    "bot",
		Text:      "🔁 Coder → @Reviewer\nplease review\n⟦nectar-relay:env_1⟧",
	})
	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		ChatType:  "group",
		MessageID: "m2",
		UserID:    "bot",
		Text:      "🔁 已转交 · Coder → @Reviewer\n任务：please review\n" + relayMarkerFor("env_2"),
	})
	router.Handle(platform.Message{
		Channel:   "feishu",
		ChatID:    "c1",
		ChatType:  "group",
		MessageID: "m3",
		UserID:    "bot",
		FromBot:   true,
		Text:      "[interactive card]",
	})
	time.Sleep(50 * time.Millisecond)
	if starts, _ := eng.counts(); starts != 0 {
		t.Fatalf("mirror triggered %d engine starts", starts)
	}
	if plat.replyCount() != 0 {
		t.Fatalf("mirror produced replies: %#v", plat.repliesSnapshot())
	}
}

func TestRelayEchoOffSuppressesTargetOutput(t *testing.T) {
	hub := newRelayTestHub(t, "off", 4)
	defer hub.Close()
	wsDir := t.TempDir()
	locks := execguard.NewWorkspaceLocks()
	wsSource, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wsTarget, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	sourcePlat := &fakePlatform{}
	targetPlat := &fakePlatform{}
	sourceEngine := &relayTestEngine{
		name: "codex",
		onStart: func(_ string, opts engine.EngineOptions) (string, error) {
			_, _, err := hub.Send(context.Background(), opts.Env["NECTAR_RELAY_TOKEN"], RelaySendRequest{
				To:   "reviewer",
				Body: "please review",
				Echo: "off",
			})
			return "asked", err
		},
		onResume: func(_ string, _ string, _ engine.EngineOptions) (string, error) {
			return "applied", nil
		},
	}
	targetEngine := &relayTestEngine{name: "claude", onStart: func(string, engine.EngineOptions) (string, error) {
		return "hidden answer", nil
	}}
	source := newRelayTestRouter(t, sourceEngine, sourcePlat, "coder", "codex", wsSource, locks, hub)
	target := newRelayTestRouter(t, targetEngine, targetPlat, "reviewer", "claude", wsTarget, locks, hub)
	hub.Register(RelayBotConfig{ID: "coder", Name: "Coder", AcceptFrom: []string{"reviewer"}, Router: source})
	hub.Register(RelayBotConfig{ID: "reviewer", Name: "Reviewer", AcceptFrom: []string{"coder"}, Router: target})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	source.Handle(platform.Message{Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", UserID: "u1", Text: "do it"})
	waitForReplyContains(t, sourcePlat, "applied")
	time.Sleep(50 * time.Millisecond)
	if got := targetPlat.replyCount(); got != 0 {
		t.Fatalf("target replies = %d, want 0: %#v", got, targetPlat.repliesSnapshot())
	}
	for _, reply := range sourcePlat.repliesSnapshot() {
		if strings.Contains(reply, "hidden answer") {
			t.Fatalf("source saw hidden target answer: %#v", sourcePlat.repliesSnapshot())
		}
	}
}

func TestRelayTargetResultUsesCard(t *testing.T) {
	hub := newRelayTestHub(t, "summary", 4)
	defer hub.Close()
	wsDir := t.TempDir()
	locks := execguard.NewWorkspaceLocks()
	wsSource, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wsTarget, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	sourcePlat := &fakePlatform{}
	targetPlat := &fakePlatform{cards: true}
	sourceEngine := &relayTestEngine{
		name: "codex",
		onStart: func(_ string, opts engine.EngineOptions) (string, error) {
			_, _, err := hub.Send(context.Background(), opts.Env["NECTAR_RELAY_TOKEN"], RelaySendRequest{
				To:   "reviewer",
				Body: "please review",
			})
			return "asked", err
		},
		onResume: func(_ string, _ string, _ engine.EngineOptions) (string, error) {
			return "applied", nil
		},
	}
	targetEngine := &relayTestEngine{name: "claude", onStart: func(string, engine.EngineOptions) (string, error) {
		return "looks good", nil
	}}
	source := newRelayTestRouter(t, sourceEngine, sourcePlat, "coder", "codex", wsSource, locks, hub)
	target := newRelayTestRouter(t, targetEngine, targetPlat, "reviewer", "claude", wsTarget, locks, hub)
	hub.Register(RelayBotConfig{ID: "coder", Name: "Coder", AcceptFrom: []string{"reviewer"}, Router: source})
	hub.Register(RelayBotConfig{ID: "reviewer", Name: "Reviewer", AcceptFrom: []string{"coder"}, Router: target})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	source.Handle(platform.Message{Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", UserID: "u1", Text: "do it"})
	waitForReplyContains(t, sourcePlat, "applied")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, card := range targetPlat.sentCardsSnapshot() {
			if card.Title == "🍯 Review Complete" && strings.Contains(card.Body, "looks good") {
				if targetPlat.replyCount() != 0 {
					t.Fatalf("target sent text alongside card: %#v", targetPlat.repliesSnapshot())
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("target cards = %#v", targetPlat.sentCardsSnapshot())
}

func TestRelayGenerationLimit(t *testing.T) {
	hub := newRelayTestHub(t, "off", 1)
	defer hub.Close()
	wsDir := t.TempDir()
	ws, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	eng := &relayTestEngine{name: "target"}
	router := newRelayTestRouter(t, eng, &fakePlatform{}, "reviewer", "claude", ws, execguard.NewWorkspaceLocks(), hub)
	coderWS, err := execguard.NewWorkspace(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	coder := newRelayTestRouter(t, &relayTestEngine{name: "coder"}, &fakePlatform{}, "coder", "codex", coderWS, execguard.NewWorkspaceLocks(), hub)
	hub.Register(RelayBotConfig{ID: "reviewer", Name: "Reviewer", AcceptFrom: []string{"coder"}, Router: router})
	hub.Register(RelayBotConfig{ID: "coder", Name: "Coder", AcceptFrom: []string{"reviewer"}, Router: coder})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := platform.Message{Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", UserID: "u1", Text: "root"}
	origin := hub.BeginHumanRun("coder", "source-session", "run-1", msg, "root")
	if origin == nil {
		t.Fatal("missing origin run")
	}
	defer hub.FinishRun(origin)
	env := RelayEnvelope{
		ID:              "env_1",
		RootID:          origin.RootID,
		OriginSessionID: "source-session",
		Channel:         "feishu",
		ChatID:          "c1",
		ChatType:        "group",
		From:            "reviewer",
		To:              "coder",
		Gen:             1,
		Kind:            "ask",
		Body:            "one",
	}
	run, err := hub.BeginTargetRun(env, "source-session")
	if err != nil {
		t.Fatal(err)
	}
	defer hub.FinishRun(run)
	_, _, err = hub.Send(context.Background(), run.Token, RelaySendRequest{To: "reviewer", Body: "two"})
	if !errors.Is(err, ErrRelayGenLimit) {
		t.Fatalf("Send() error = %v, want ErrRelayGenLimit", err)
	}
}

func TestRelayIdempotency(t *testing.T) {
	hub := newRelayTestHub(t, "off", 4)
	defer hub.Close()
	wsDir := t.TempDir()
	ws, err := execguard.NewWorkspace(wsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	block := &blockingEngine{startCh: make(chan struct{}), release: make(chan struct{})}
	router := newRelayTestRouter(t, block, &fakePlatform{}, "reviewer", "claude", ws, execguard.NewWorkspaceLocks(), hub)
	hub.Register(RelayBotConfig{ID: "reviewer", Name: "Reviewer", AcceptFrom: []string{"coder"}, Router: router})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := platform.Message{Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", UserID: "u1", Text: "root"}
	origin := hub.BeginHumanRun("coder", "source-session", "run-1", msg, "root")
	if origin == nil {
		t.Fatal("missing origin run")
	}
	defer hub.FinishRun(origin)
	first, dup, err := hub.Send(context.Background(), origin.Token, RelaySendRequest{To: "reviewer", Body: "same body"})
	if err != nil {
		t.Fatal(err)
	}
	if dup {
		t.Fatal("first send marked duplicate")
	}
	second, dup, err := hub.Send(context.Background(), origin.Token, RelaySendRequest{To: "reviewer", Body: "same body"})
	if err != nil {
		t.Fatal(err)
	}
	if !dup || second.ID != first.ID {
		t.Fatalf("second send = %#v duplicate=%v, want %s", second, dup, first.ID)
	}
	_, envelopes, ok := hub.Root(origin.RootID)
	if !ok || len(envelopes) != 1 {
		t.Fatalf("envelopes = %#v, ok = %v", envelopes, ok)
	}
	close(block.release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, current, _ := hub.Root(origin.RootID)
		if len(current) == 1 && current[0].State == "done" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("target run did not finish")
}

func TestRelayHTTPAPI(t *testing.T) {
	hub := newRelayTestHub(t, "off", 4)
	defer hub.Close()
	ws, err := execguard.NewWorkspace(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	eng := &relayTestEngine{name: "reviewer", onStart: func(string, engine.EngineOptions) (string, error) {
		return "api answer", nil
	}}
	router := newRelayTestRouter(t, eng, &fakePlatform{}, "reviewer", "claude", ws, execguard.NewWorkspaceLocks(), hub)
	hub.Register(RelayBotConfig{ID: "reviewer", Name: "Reviewer", AcceptFrom: []string{"coder"}, Router: router})
	if err := hub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := platform.Message{Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", UserID: "u1", Text: "root"}
	origin := hub.BeginHumanRun("coder", "source-session", "run-1", msg, "root")
	if origin == nil {
		t.Fatal("missing origin run")
	}
	defer hub.FinishRun(origin)

	api := NewScheduleAPI(ScheduleAPIOptions{
		Relay:  hub,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	body, _ := json.Marshal(RelaySendRequest{To: "reviewer", Body: "api hello"})
	req, err := http.NewRequest(http.MethodPost, server.URL+"/relay/send", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+origin.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var response struct {
		Envelope  RelayEnvelope `json:"envelope"`
		Duplicate bool          `json:"duplicate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Envelope.ID == "" || response.Duplicate {
		t.Fatalf("response = %#v", response)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, envelopes, ok := hub.Root(response.Envelope.RootID)
		if ok && len(envelopes) == 1 && envelopes[0].State == "done" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("relay envelope did not finish")
}

func TestRelayReplaysPendingEnvelopeAfterRestart(t *testing.T) {
	dataDir := t.TempDir()
	hub1, err := NewRelayHub(RelayOptions{
		Enabled:             true,
		Echo:                "off",
		MaxGeneration:       4,
		MaxEnvelopesPerRoot: 12,
		MaxRunsPerRoot:      8,
		RootTTL:             30 * time.Minute,
		DataDir:             dataDir,
		Logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	hub1.mu.Lock()
	hub1.roots["root_replay"] = &relayRoot{
		ID:           "root_replay",
		OriginBot:    "coder",
		Channel:      "feishu",
		ChatID:       "c1",
		ChatType:     "group",
		OriginPrompt: "original task",
		CreatedAt:    now,
		LastActivity: now,
		State:        "active",
	}
	hub1.envelopes["env_replay"] = &RelayEnvelope{
		ID:        "env_replay",
		RootID:    "root_replay",
		Channel:   "feishu",
		ChatID:    "c1",
		ChatType:  "group",
		From:      "coder",
		To:        "reviewer",
		Gen:       1,
		Kind:      "ask",
		Body:      "replayed request",
		Echo:      "off",
		CreatedAt: now,
		State:     "pending",
	}
	if err := hub1.persistLocked(); err != nil {
		hub1.mu.Unlock()
		t.Fatal(err)
	}
	hub1.mu.Unlock()
	hub1.Close()

	hub2, err := NewRelayHub(RelayOptions{
		Enabled:             true,
		Echo:                "off",
		MaxGeneration:       4,
		MaxEnvelopesPerRoot: 12,
		MaxRunsPerRoot:      8,
		RootTTL:             30 * time.Minute,
		DataDir:             dataDir,
		Logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer hub2.Close()
	ws, err := execguard.NewWorkspace(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	eng := &relayTestEngine{name: "reviewer", onStart: func(string, engine.EngineOptions) (string, error) {
		return "replayed answer", nil
	}}
	router := newRelayTestRouter(t, eng, &fakePlatform{}, "reviewer", "claude", ws, execguard.NewWorkspaceLocks(), hub2)
	hub2.Register(RelayBotConfig{ID: "reviewer", Name: "Reviewer", AcceptFrom: []string{"coder"}, Router: router})
	if err := hub2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, envelopes, _ := hub2.Root("root_replay")
		if len(envelopes) == 1 && envelopes[0].State == "done" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("pending relay envelope was not replayed to completion")
}
