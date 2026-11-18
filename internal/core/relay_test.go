package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

type relayRig struct {
	hub          *RelayHub
	coderRouter  *Router
	reviewRouter *Router
	coderEng     *fakeEngine
	reviewEng    *fakeEngine
	coderPlat    *fakePlatform
	reviewPlat   *fakePlatform
	coderLessons *comb.FileStore
	reviewLesson *comb.FileStore
}

func newRelayRig(t *testing.T, opts RelayHubOptions) *relayRig {
	t.Helper()
	if opts.DataDir == "" {
		opts.DataDir = t.TempDir()
	}
	if opts.Echo == "" {
		opts.Echo = RelayEchoSummary
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	opts.Logger = logger
	hub, err := NewRelayHub(opts)
	if err != nil {
		t.Fatal(err)
	}
	hub.opts.FirstMentionOnly = true
	rig := &relayRig{
		hub:        hub,
		coderEng:   &fakeEngine{},
		reviewEng:  &fakeEngine{},
		coderPlat:  &fakePlatform{},
		reviewPlat: &fakePlatform{},
	}
	rig.coderRouter, rig.coderLessons = newRelayRouter(t, "coder", hub, rig.coderEng, rig.coderPlat)
	rig.reviewRouter, rig.reviewLesson = newRelayRouter(t, "reviewer", hub, rig.reviewEng, rig.reviewPlat)
	hub.Register(RelayPeer{ID: "coder", Name: "Coder", Role: "coder"}, rig.coderRouter)
	hub.Register(RelayPeer{ID: "reviewer", Name: "Reviewer", Role: "reviewer"}, rig.reviewRouter)
	hub.NoteChat("c1", "coder")
	hub.NoteChat("c1", "reviewer")
	t.Cleanup(hub.Close)
	return rig
}

func newRelayRouter(t *testing.T, botID string, hub *RelayHub, eng engine.EngineAdapter, plat platform.PlatformAdapter) (*Router, *comb.FileStore) {
	t.Helper()
	return newRelayRouterWithSocket(t, botID, hub, eng, plat, "")
}

func newRelayRouterWithSocket(t *testing.T, botID string, hub *RelayHub, eng engine.EngineAdapter, plat platform.PlatformAdapter, socket string) (*Router, *comb.FileStore) {
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
		nil,
		decision.Noop{},
		logger,
		RouterConfig{BotID: botID, AgentID: "codex", BotName: botID, APISocket: socket, MemoryDefaultScope: "chat"},
	)
	router.SetRelay(hub)
	return router, lessons
}

func relayKey(botID string) SessionKey {
	return SessionKey{BotID: botID, Channel: "feishu", ChatID: "c1", AgentID: "codex"}.normalized()
}

func waitRelay(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func relayEnvelopeState(hub *RelayHub, id string) string {
	for _, root := range hub.Roots() {
		for _, envelope := range root.Envelopes {
			if envelope.ID == id {
				return envelope.State
			}
		}
	}
	return ""
}

func TestRelayAskDeliversAnswerToOriginSession(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})

	rig.coderRouter.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "write a patch"})
	waitForReplies(t, rig.coderPlat, 1)
	originID := rig.coderRouter.sessions.ActiveID(relayKey("coder"))
	if originID == "" {
		t.Fatal("coder has no active session")
	}

	token := rig.hub.MintToken(RelayTokenRequest{
		BotID: "coder", SessionID: originID, RunID: "run-1", Prompt: "write a patch",
		Channel: "feishu", ChatID: "c1",
	})
	binding := rig.hub.TokenBinding(token)
	envelope, err := rig.hub.Send(binding, RelaySendRequest{To: "reviewer", Body: "review this diff"})
	if err != nil {
		t.Fatal(err)
	}
	waitRelay(t, "reviewer run", func() bool {
		if starts, _, _ := rig.reviewEng.counts(); starts != 1 {
			return false
		}
		return relayEnvelopeState(rig.hub, envelope.ID) == RelayStateDone
	})
	waitRelay(t, "origin reply turn", func() bool {
		_, resumes, _ := rig.coderEng.counts()
		return resumes == 1
	})

	if got := rig.coderRouter.sessions.ActiveID(relayKey("coder")); got != originID {
		t.Fatalf("relay hijacked coder active session: %q != %q", got, originID)
	}
	if got := rig.reviewRouter.sessions.ActiveID(relayKey("reviewer")); got != "" {
		t.Fatalf("relay target set an active session: %q", got)
	}
	sessions := rig.reviewRouter.sessions.List(relayKey("reviewer"), false)
	if len(sessions) != 1 || !strings.HasPrefix(sessions[0].Name, "relay:") {
		t.Fatalf("reviewer sessions = %#v", sessions)
	}
	waitForReplies(t, rig.coderPlat, 2)
	if got := rig.coderPlat.lastReply(); got != "answer" {
		t.Fatalf("reply turn answer = %q", got)
	}
	// The target's echo=summary answer is visible exactly once.
	waitForReplies(t, rig.reviewPlat, 1)
	prompt, _ := rig.reviewEng.lastRequest()
	if !strings.Contains(prompt, "<relay-request") || !strings.Contains(prompt, "review this diff") {
		t.Fatalf("target prompt missing relay fence: %q", prompt)
	}
	if opts := rig.reviewEng.lastOpts; opts.Env["NECTAR_RELAY_TOKEN"] == "" || opts.Env["NECTAR_RELAY_ROOT"] == "" {
		t.Fatalf("target run missing relay env: %#v", opts.Env)
	}
}

func TestRelayGenerationLimit(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{MaxGeneration: 1})
	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1"})
	binding := rig.hub.TokenBinding(token)
	first, err := rig.hub.Send(binding, RelaySendRequest{To: "reviewer", Body: "first hop"})
	if err != nil {
		t.Fatalf("first hop: %v", err)
	}
	waitRelay(t, "first hop", func() bool { return relayEnvelopeState(rig.hub, first.ID) == RelayStateDone })

	second := rig.hub.MintToken(RelayTokenRequest{
		BotID: "reviewer", SessionID: "s2", RootID: first.RootID, Gen: 1,
		Channel: "feishu", ChatID: "c1",
	})
	if _, err := rig.hub.Send(rig.hub.TokenBinding(second), RelaySendRequest{To: "coder", Body: "second hop"}); RelayErrorCode(err) != ErrRelayGenLimit {
		t.Fatalf("second hop error = %v", err)
	}
}

func TestRelayIdempotentSend(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1"})
	binding := rig.hub.TokenBinding(token)
	request := RelaySendRequest{To: "reviewer", Body: "review me", IdempotencyKey: "k1"}
	first, err := rig.hub.Send(binding, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := rig.hub.Send(binding, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent send returned %q then %q", first.ID, second.ID)
	}
	waitRelay(t, "single reviewer run", func() bool {
		starts, _, _ := rig.reviewEng.counts()
		return starts == 1 && relayEnvelopeState(rig.hub, first.ID) == RelayStateDone
	})
	time.Sleep(50 * time.Millisecond)
	if starts, _, _ := rig.reviewEng.counts(); starts != 1 {
		t.Fatalf("reviewer ran %d times", starts)
	}
}

func TestRelayHandoffDoesNotReply(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1"})
	envelope, err := rig.hub.Send(rig.hub.TokenBinding(token), RelaySendRequest{To: "reviewer", Body: "take over", NoReply: true})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Kind != RelayKindHandoff {
		t.Fatalf("kind = %q", envelope.Kind)
	}
	waitRelay(t, "handoff run", func() bool { return relayEnvelopeState(rig.hub, envelope.ID) == RelayStateDone })
	time.Sleep(50 * time.Millisecond)
	if _, resumes, _ := rig.coderEng.counts(); resumes != 0 {
		t.Fatalf("handoff woke the origin session %d times", resumes)
	}
}

func TestRelayRejectsBadTargets(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})

	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1"})
	if _, err := rig.hub.Send(rig.hub.TokenBinding(token), RelaySendRequest{To: "ghost", Body: "hi"}); RelayErrorCode(err) != ErrRelayTargetUnknown {
		t.Fatalf("unknown target error = %v", err)
	}
	if _, err := rig.hub.Send(rig.hub.TokenBinding(token), RelaySendRequest{To: "reviewer", Body: "hi"}); err != nil {
		t.Fatalf("known target rejected: %v", err)
	}

	otherChat := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s2", Channel: "feishu", ChatID: "c2"})
	if _, err := rig.hub.Send(rig.hub.TokenBinding(otherChat), RelaySendRequest{To: "reviewer", Body: "hi"}); RelayErrorCode(err) != ErrRelayNotInChat {
		t.Fatalf("not-in-chat error = %v", err)
	}

	strict := newRelayRig(t, RelayHubOptions{})
	strict.hub.Register(RelayPeer{ID: "reviewer", Name: "Reviewer", AcceptFrom: []string{"lead"}}, strict.reviewRouter)
	strictToken := strict.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1"})
	if _, err := strict.hub.Send(strict.hub.TokenBinding(strictToken), RelaySendRequest{To: "reviewer", Body: "hi"}); RelayErrorCode(err) != ErrRelayRejected {
		t.Fatalf("accept_from error = %v", err)
	}
}

func TestRelayRejectsExpiredToken(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1"})
	rig.hub.RevokeToken(token)
	if _, err := rig.hub.Send(rig.hub.TokenBinding(token), RelaySendRequest{To: "reviewer", Body: "hi"}); RelayErrorCode(err) != ErrRelayUnauthorized {
		t.Fatalf("revoked token error = %v", err)
	}
}

func TestRouterDropsBotAndMirrorMessages(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	rig.coderRouter.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "hello", FromBot: true})
	rig.coderRouter.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "hello " + relayMirrorMarker()})
	time.Sleep(50 * time.Millisecond)
	if starts, resumes, _ := rig.coderEng.counts(); starts+resumes != 0 {
		t.Fatalf("dropped messages still ran the engine: starts=%d resumes=%d", starts, resumes)
	}
	if rig.coderPlat.replyCount() != 0 {
		t.Fatalf("dropped messages produced replies: %d", rig.coderPlat.replyCount())
	}
}

func TestRelayBodySkipsSlashCommands(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	envelope := RelayEnvelope{
		ID: "env_cmd", RootID: "root_cmd", Kind: RelayKindAsk,
		Channel: "feishu", ChatID: "c1", From: "coder", To: "reviewer", Gen: 1,
		Body: "/cron add --cron '0 9 * * *' --prompt x",
	}
	result, err := rig.reviewRouter.ExecuteRelay(context.Background(), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "answer" {
		t.Fatalf("answer = %q", result.Answer)
	}
	starts, _, _ := rig.reviewEng.counts()
	if starts != 1 {
		t.Fatalf("reviewer engine starts = %d; relay body was parsed as a command", starts)
	}
	prompt, _ := rig.reviewEng.lastRequest()
	if !strings.Contains(prompt, "/cron add") {
		t.Fatalf("prompt lost the relay body: %q", prompt)
	}
}

func TestRelayRunSkipsLessons(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	rig.reviewEng.fail = true
	envelope := RelayEnvelope{
		ID: "env_fail", RootID: "root_fail", Kind: RelayKindAsk,
		Channel: "feishu", ChatID: "c1", From: "coder", To: "reviewer", Gen: 1,
		Body: "break things",
	}
	if _, err := rig.reviewRouter.ExecuteRelay(context.Background(), envelope); err == nil {
		t.Fatal("expected a failed relay run")
	}
	pending, err := rig.reviewLesson.ListPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("relay run recorded %d lessons", len(pending))
	}
}

func TestRelayHubRedeliversPendingAfterRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	roots := map[string]*RelayRoot{
		"root_seed": {
			ID: "root_seed", OriginBot: "coder", OriginSessionID: "origin-1",
			Channel: "feishu", ChatID: "c1", State: relayRootOpen, CreatedAt: now,
		},
	}
	envelopes := map[string]*RelayEnvelope{
		"env_seed": {
			ID: "env_seed", RootID: "root_seed", Kind: RelayKindHandoff,
			Channel: "feishu", ChatID: "c1", From: "coder", To: "reviewer", Gen: 1,
			Body: "resume this", State: RelayStatePending, CreatedAt: now,
		},
	}
	data, err := marshalRelayState(roots, envelopes)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRelayStateFile(filepath.Join(dir, "relay", "state.json"), data); err != nil {
		t.Fatal(err)
	}

	rig := newRelayRig(t, RelayHubOptions{DataDir: dir})
	rig.hub.Start(context.Background())
	waitRelay(t, "pending redelivery", func() bool {
		starts, _, _ := rig.reviewEng.counts()
		return starts == 1 && relayEnvelopeState(rig.hub, "env_seed") == RelayStateDone
	})
}

func TestRelayHubFailsRunningAfterRestart(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	roots := map[string]*RelayRoot{
		"root_busy": {
			ID: "root_busy", OriginBot: "coder", OriginSessionID: "origin-1",
			Channel: "feishu", ChatID: "c1", State: relayRootOpen, CreatedAt: now,
		},
	}
	envelopes := map[string]*RelayEnvelope{
		"env_busy": {
			ID: "env_busy", RootID: "root_busy", Kind: RelayKindHandoff,
			Channel: "feishu", ChatID: "c1", From: "coder", To: "reviewer", Gen: 1,
			Body: "was running", State: RelayStateRunning, CreatedAt: now,
		},
	}
	data, err := marshalRelayState(roots, envelopes)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRelayStateFile(filepath.Join(dir, "relay", "state.json"), data); err != nil {
		t.Fatal(err)
	}

	rig := newRelayRig(t, RelayHubOptions{DataDir: dir})
	rig.hub.Start(context.Background())
	waitRelay(t, "running envelope failed", func() bool {
		return relayEnvelopeState(rig.hub, "env_busy") == RelayStateFailed
	})
	time.Sleep(50 * time.Millisecond)
	if starts, _, _ := rig.reviewEng.counts(); starts != 0 {
		t.Fatalf("interrupted envelope was re-run %d times", starts)
	}
}

func TestRelayCancelRootStopsPendingWork(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1"})
	envelope, err := rig.hub.Send(rig.hub.TokenBinding(token), RelaySendRequest{To: "reviewer", Body: "review me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.hub.CancelRoot(envelope.RootID); err != nil {
		t.Fatal(err)
	}
	root := rig.hub.Root(envelope.RootID)
	if root == nil || root.State != relayRootClosed {
		t.Fatalf("root after cancel = %#v", root)
	}
}

func TestRelayPeersReportsRoster(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	peers := rig.hub.Peers("c1")
	if len(peers) != 2 {
		t.Fatalf("peers = %#v", peers)
	}
	seen := map[string]bool{}
	for _, peer := range peers {
		seen[peer.ID] = true
		if peer.Role == "" {
			t.Fatalf("peer %q has no role", peer.ID)
		}
	}
	if !seen["coder"] || !seen["reviewer"] {
		t.Fatalf("roster = %#v", peers)
	}
}

func writeRelayStateFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func TestRelayAPIOverUnixSocket(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	socket := filepath.Join(t.TempDir(), "api.sock")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := NewScheduleAPI(ScheduleAPIOptions{SocketPath: socket, Relay: rig.hub, Logger: logger})
	if err := api.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer api.Close()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socket)
			},
		},
		Timeout: 5 * time.Second,
	}
	do := func(method, path, token string, body any) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(data)
		}
		req, err := http.NewRequest(method, "http://nectar"+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, data
	}

	status, data := do(http.MethodGet, "/relay/bots", "", nil)
	if status != http.StatusOK {
		t.Fatalf("bots status = %d: %s", status, data)
	}
	var roster struct {
		Bots []RelayPeer `json:"bots"`
	}
	if err := json.Unmarshal(data, &roster); err != nil {
		t.Fatal(err)
	}
	if len(roster.Bots) != 2 {
		t.Fatalf("roster = %s", data)
	}

	status, data = do(http.MethodPost, "/relay/send", "", RelaySendRequest{To: "reviewer", Body: "hi"})
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated send status = %d: %s", status, data)
	}

	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1"})
	status, data = do(http.MethodPost, "/relay/send", token, RelaySendRequest{To: "reviewer", Body: "review this"})
	if status != http.StatusOK {
		t.Fatalf("send status = %d: %s", status, data)
	}
	var envelope RelayEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ID == "" || envelope.RootID == "" || envelope.Kind != RelayKindAsk {
		t.Fatalf("envelope = %s", data)
	}

	status, data = do(http.MethodGet, "/relay/roots", "", nil)
	if status != http.StatusOK || !strings.Contains(string(data), envelope.RootID) {
		t.Fatalf("roots status = %d: %s", status, data)
	}
	status, data = do(http.MethodGet, "/relay/roots/"+envelope.RootID, "", nil)
	if status != http.StatusOK || !strings.Contains(string(data), envelope.ID) {
		t.Fatalf("root status = %d: %s", status, data)
	}
	status, _ = do(http.MethodPost, "/relay/roots/"+envelope.RootID+"/cancel", "", nil)
	if status != http.StatusOK {
		t.Fatalf("cancel status = %d", status)
	}
}

func TestRelayMirrorMarkerIsInvisible(t *testing.T) {
	marker := relayMirrorMarker()
	if strings.Contains(marker, "nectar") || strings.Contains(marker, "relay") {
		t.Fatalf("marker contains visible text: %q", marker)
	}
	rig := newRelayRig(t, RelayHubOptions{})
	if !rig.hub.IsMirrorText("handoff " + marker) {
		t.Fatal("invisible marker was not recognized")
	}
	envelope := RelayEnvelope{ID: "env_mirror", ChatID: "c1", Body: "review this diff"}
	if _, err := rig.coderRouter.SendRelayMirror(context.Background(), envelope, "Reviewer"); err != nil {
		t.Fatal(err)
	}
	if got := rig.coderPlat.lastReply(); strings.Contains(got, "nectar-relay") || strings.Contains(got, "env_mirror") {
		t.Fatalf("mirror leaks plumbing: %q", got)
	}
}

func TestRelayRejectedSendLeavesNoRoot(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c9"})
	_, err := rig.hub.Send(rig.hub.TokenBinding(token), RelaySendRequest{To: "reviewer", Body: "hi"})
	if RelayErrorCode(err) != ErrRelayNotInChat {
		t.Fatalf("error = %v", err)
	}
	if roots := rig.hub.Roots(); len(roots) != 0 {
		t.Fatalf("rejected send left %d roots", len(roots))
	}
}

func TestRelayLoadPrunesEmptyRoots(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	roots := map[string]*RelayRoot{
		"root_empty": {ID: "root_empty", OriginBot: "coder", Channel: "feishu", ChatID: "c1", State: relayRootOpen, CreatedAt: now},
		"root_used":  {ID: "root_used", OriginBot: "coder", Channel: "feishu", ChatID: "c1", State: relayRootOpen, CreatedAt: now},
	}
	envelopes := map[string]*RelayEnvelope{
		"env_used": {ID: "env_used", RootID: "root_used", Kind: RelayKindHandoff, Channel: "feishu", ChatID: "c1", State: RelayStateDone, CreatedAt: now},
	}
	data, err := marshalRelayState(roots, envelopes)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRelayStateFile(filepath.Join(dir, "relay", "state.json"), data); err != nil {
		t.Fatal(err)
	}
	hub, err := NewRelayHub(RelayHubOptions{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	loaded := hub.Roots()
	if len(loaded) != 1 || loaded[0].ID != "root_used" {
		t.Fatalf("roots after prune = %#v", loaded)
	}
	if loaded[0].EnvelopeCount != 1 {
		t.Fatalf("envelope count = %d", loaded[0].EnvelopeCount)
	}
}

func TestRelayRejectsMutualRelay(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	roots := map[string]*RelayRoot{
		"root_mutual": {
			ID: "root_mutual", OriginBot: "reviewer", OriginSessionID: "s-review",
			Channel: "feishu", ChatID: "c1", OriginMessageID: "om_same",
			State: relayRootOpen, CreatedAt: now,
		},
	}
	envelopes := map[string]*RelayEnvelope{
		"env_pending": {
			ID: "env_pending", RootID: "root_mutual", Kind: RelayKindAsk,
			Channel: "feishu", ChatID: "c1", From: "reviewer", To: "coder", Gen: 1,
			State: RelayStateRunning, CreatedAt: now,
		},
	}
	data, err := marshalRelayState(roots, envelopes)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRelayStateFile(filepath.Join(dir, "relay", "state.json"), data); err != nil {
		t.Fatal(err)
	}
	rig := newRelayRig(t, RelayHubOptions{DataDir: dir})
	token := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1", OriginMessageID: "om_same"})
	if _, err := rig.hub.Send(rig.hub.TokenBinding(token), RelaySendRequest{To: "reviewer", Body: "also review mine"}); RelayErrorCode(err) != ErrRelayMutual {
		t.Fatalf("mutual relay error = %v", err)
	}
	// A different human message is independent work and is allowed.
	independent := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c1", OriginMessageID: "om_other"})
	if _, err := rig.hub.Send(rig.hub.TokenBinding(independent), RelaySendRequest{To: "reviewer", Body: "unrelated task"}); err != nil {
		t.Fatalf("independent message rejected: %v", err)
	}
	// A different chat is unaffected.
	rig.hub.NoteChat("c2", "coder")
	rig.hub.NoteChat("c2", "reviewer")
	other := rig.hub.MintToken(RelayTokenRequest{BotID: "coder", SessionID: "s1", Channel: "feishu", ChatID: "c2", OriginMessageID: "om_same"})
	if _, err := rig.hub.Send(rig.hub.TokenBinding(other), RelaySendRequest{To: "reviewer", Body: "different chat"}); err != nil {
		t.Fatalf("different chat rejected: %v", err)
	}
}

func TestRelayBotNameMatchingToleratesDecorations(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	if id, ok := rig.hub.BotIDByName("Coder"); !ok || id != "coder" {
		t.Fatalf("exact match = %q, %v", id, ok)
	}
	if id, ok := rig.hub.BotIDByName("Reviewer 🦞"); !ok || id != "reviewer" {
		t.Fatalf("decorated match = %q, %v", id, ok)
	}
	if _, ok := rig.hub.BotIDByName("张三"); ok {
		t.Fatal("human name matched a bot")
	}
}

func TestRouterFirstMentionWithDecoratedPeerName(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	msg := platform.Message{
		Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", Text: "please help",
		Mentions: []platform.Mention{
			{Name: "Reviewer 🦞", IsBot: true},
			{Name: "Coder", IsBot: true},
		},
	}
	rig.coderRouter.Handle(msg)
	time.Sleep(50 * time.Millisecond)
	if starts, resumes, _ := rig.coderEng.counts(); starts+resumes != 0 {
		t.Fatalf("coder acted on a message addressed to the decorated peer: starts=%d resumes=%d", starts, resumes)
	}
}

func TestRouterFirstMentionOnly(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	msg := platform.Message{
		Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", Text: "please help",
		Mentions: []platform.Mention{
			{Name: "Coder", IsBot: true},
			{Name: "Reviewer", IsBot: true},
		},
	}
	rig.reviewRouter.Handle(msg)
	time.Sleep(50 * time.Millisecond)
	if starts, resumes, _ := rig.reviewEng.counts(); starts+resumes != 0 {
		t.Fatalf("reviewer acted on a message addressed to coder: starts=%d resumes=%d", starts, resumes)
	}
	rig.coderRouter.Handle(msg)
	waitForReplies(t, rig.coderPlat, 1)
	if starts, _, _ := rig.coderEng.counts(); starts != 1 {
		t.Fatalf("coder engine starts = %d", starts)
	}
}

func TestRouterFirstMentionIgnoresHumanMentions(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	msg := platform.Message{
		Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", Text: "please help",
		Mentions: []platform.Mention{
			{Name: "张三", IsBot: false},
			{Name: "Reviewer", IsBot: true},
		},
	}
	rig.reviewRouter.Handle(msg)
	waitForReplies(t, rig.reviewPlat, 1)
}

func TestRouterFirstMentionCanBeDisabled(t *testing.T) {
	rig := newRelayRig(t, RelayHubOptions{})
	rig.hub.opts.FirstMentionOnly = false
	msg := platform.Message{
		Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1", Text: "please help",
		Mentions: []platform.Mention{
			{Name: "Coder", IsBot: true},
			{Name: "Reviewer", IsBot: true},
		},
	}
	rig.reviewRouter.Handle(msg)
	waitForReplies(t, rig.reviewPlat, 1)
}

// relayPostingEngine imitates an agent that reacts to its prompt by calling the
// relay CLI: it reads the run token and socket from the environment and posts
// one ask through the real local API. Everything after that (hub validation,
// target run, reply back to the origin session) runs through production code.
type relayPostingEngine struct {
	mu      sync.Mutex
	starts  int
	resumes int
	token   string
	postErr error
}

func (e *relayPostingEngine) Name() string { return "poster" }

func (e *relayPostingEngine) Start(_ context.Context, _ string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	e.mu.Lock()
	e.starts++
	e.token = opts.Env["NECTAR_RELAY_TOKEN"]
	socket := opts.Env["NECTAR_API_SOCKET"]
	e.mu.Unlock()
	if socket == "" {
		e.setErr(errors.New("NECTAR_API_SOCKET is empty"))
	} else {
		e.setErr(postRelayViaSocket(socket, e.token, "reviewer", "please review the diff"))
	}
	return relayScriptedEvents("impl-done"), nil
}

func (e *relayPostingEngine) Resume(_ context.Context, _, _ string, _ engine.EngineOptions) (<-chan engine.Event, error) {
	e.mu.Lock()
	e.resumes++
	e.mu.Unlock()
	return relayScriptedEvents("review-summary"), nil
}

func (e *relayPostingEngine) Cancel(context.Context, string) error { return nil }

func (e *relayPostingEngine) setErr(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.postErr == nil {
		e.postErr = err
	}
}

func (e *relayPostingEngine) counts() (int, int, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts, e.resumes, e.token, e.postErr
}

func relayScriptedEvents(answer string) <-chan engine.Event {
	ch := make(chan engine.Event, 3)
	ch <- engine.Event{Type: engine.EventSessionStarted, SessionID: "relay-e2e"}
	ch <- engine.Event{Type: engine.EventMessage, Text: answer}
	ch <- engine.Event{Type: engine.EventDone}
	close(ch)
	return ch
}

func postRelayViaSocket(socket, token, to, body string) error {
	payload, err := json.Marshal(map[string]string{"to": to, "body": body})
	if err != nil {
		return err
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socket)
			},
		},
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest(http.MethodPost, "http://nectar/relay/send", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("relay send status %d: %s", resp.StatusCode, data)
	}
	return nil
}

func TestRelayEndToEndThroughLocalAPI(t *testing.T) {
	hub, err := NewRelayHub(RelayHubOptions{DataDir: t.TempDir(), Echo: RelayEchoSummary, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	hub.opts.FirstMentionOnly = true
	defer hub.Close()
	socketDir, err := os.MkdirTemp("/tmp", "nectar-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "api.sock")
	api := NewScheduleAPI(ScheduleAPIOptions{SocketPath: socket, Relay: hub, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := api.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer api.Close()

	poster := &relayPostingEngine{}
	coderPlat := &fakePlatform{}
	reviewPlat := &fakePlatform{}
	reviewEng := &fakeEngine{}
	coderRouter, _ := newRelayRouterWithSocket(t, "coder", hub, poster, coderPlat, socket)
	reviewRouter, _ := newRelayRouter(t, "reviewer", hub, reviewEng, reviewPlat)
	hub.Register(RelayPeer{ID: "coder", Name: "Coder", Role: "coder"}, coderRouter)
	hub.Register(RelayPeer{ID: "reviewer", Name: "Reviewer", Role: "reviewer"}, reviewRouter)
	hub.NoteChat("c1", "coder")
	hub.NoteChat("c1", "reviewer")

	// The human mentions both bots; only the first one is supposed to act.
	coderRouter.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1",
		Text: "implement it, then have reviewer check it",
		Mentions: []platform.Mention{
			{Name: "Coder", IsBot: true},
			{Name: "Reviewer", IsBot: true},
		},
	})
	reviewRouter.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", ChatType: "group", MessageID: "m1",
		Text: "implement it, then have reviewer check it",
		Mentions: []platform.Mention{
			{Name: "Coder", IsBot: true},
			{Name: "Reviewer", IsBot: true},
		},
	})

	waitRelay(t, "coder posts through the API and gets the reply turn", func() bool {
		starts, resumes, _, _ := poster.counts()
		return starts == 1 && resumes == 1
	})
	waitForReplies(t, reviewPlat, 1)
	waitForReplies(t, coderPlat, 2)

	starts, resumes, token, postErr := poster.counts()
	if postErr != nil {
		t.Fatalf("relay send through API failed: %v", postErr)
	}
	if token == "" {
		t.Fatal("coder run had no relay token")
	}
	if starts != 1 || resumes != 1 {
		t.Fatalf("coder runs: starts=%d resumes=%d", starts, resumes)
	}
	if starts, _, _ := reviewEng.counts(); starts != 1 {
		t.Fatalf("reviewer engine starts = %d", starts)
	}
	roots := hub.Roots()
	if len(roots) != 1 {
		t.Fatalf("roots = %d", len(roots))
	}
	if roots[0].EnvelopeCount != 2 || roots[0].RunCount != 2 {
		t.Fatalf("root counts = env %d runs %d", roots[0].EnvelopeCount, roots[0].RunCount)
	}
	for _, envelope := range roots[0].Envelopes {
		if envelope.State != RelayStateDone {
			t.Fatalf("envelope %s state = %s error=%s", envelope.ID, envelope.State, envelope.Error)
		}
	}
	reviewSessions := reviewRouter.sessions.List(relayKey("reviewer"), false)
	if len(reviewSessions) != 1 || !strings.HasPrefix(reviewSessions[0].Name, "relay:") {
		t.Fatalf("reviewer sessions = %#v", reviewSessions)
	}
	for _, reply := range append(fakeReplies(coderPlat), fakeReplies(reviewPlat)...) {
		if strings.Contains(reply, "nectar-relay") {
			t.Fatalf("reply leaks the mirror marker: %q", reply)
		}
	}
}

func fakeReplies(plat *fakePlatform) []string {
	plat.mu.Lock()
	defer plat.mu.Unlock()
	return append([]string(nil), plat.replies...)
}
