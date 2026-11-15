package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestRouterSessionCommandsPreserveAndSwitch(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "first task"})
	waitForReplies(t, plat, 1)
	first := router.sessions.List(key, false)[0]
	if first.Name != "first task" {
		t.Fatalf("first session name = %q", first.Name)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/new second"})
	waitForReplies(t, plat, 2)
	second, ok := router.sessions.Active(key)
	if !ok || second.Name != "second" || second.ID == first.ID {
		t.Fatalf("new session = %#v, ok=%v", second, ok)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m3", Text: "second task"})
	waitForReplies(t, plat, 3)
	if starts, resumes, _ := eng.counts(); starts != 2 || resumes != 0 {
		t.Fatalf("after new session starts=%d resumes=%d", starts, resumes)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m4", Text: "/list"})
	waitForReplies(t, plat, 4)
	listReply := plat.lastReply()
	for _, want := range []string{"first task", "second", "1:", "active"} {
		if !strings.Contains(listReply, want) {
			t.Fatalf("list reply = %q, want %q", listReply, want)
		}
	}
	for _, unwanted := range []string{"* 1.", "\n  2.", "<id>", "<name>"} {
		if strings.Contains(listReply, unwanted) {
			t.Fatalf("list reply contains unsafe markdown text %q: %q", unwanted, listReply)
		}
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m5", Text: "/switch " + first.ID})
	waitForReplies(t, plat, 5)
	if got := router.sessions.ActiveID(key); got != first.ID {
		t.Fatalf("active after switch = %q, want %q", got, first.ID)
	}
	if reply := plat.lastReply(); !strings.Contains(reply, "Name: first task") {
		t.Fatalf("switch reply = %q", reply)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m6", Text: "continue"})
	waitForReplies(t, plat, 6)
	if _, resumes, _ := eng.counts(); resumes != 1 {
		t.Fatalf("switched session resumes = %d, want 1", resumes)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m7", Text: "/rename renamed"})
	waitForReplies(t, plat, 7)
	renamed, _ := router.sessions.GetSession(first.ID)
	if renamed.Name != "renamed" {
		t.Fatalf("renamed session = %#v", renamed)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m8", Text: "/current"})
	waitForReplies(t, plat, 8)
	if reply := plat.lastReply(); !strings.Contains(reply, "Name: renamed") || !strings.Contains(reply, first.ID) {
		t.Fatalf("current reply = %q", reply)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m9", Text: "/archive"})
	waitForReplies(t, plat, 9)
	if got := router.sessions.ActiveID(key); got != "" {
		t.Fatalf("active after archive = %q", got)
	}
	if reply := plat.lastReply(); !strings.Contains(reply, "Archived") {
		t.Fatalf("archive reply = %q", reply)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m10", Text: "after archive"})
	waitForReplies(t, plat, 10)
	if starts, _, _ := eng.counts(); starts != 3 {
		t.Fatalf("after archive starts = %d, want 3", starts)
	}
	if active, ok := router.sessions.Active(key); !ok || active.ID == first.ID || active.ID == second.ID {
		t.Fatalf("new active session after archive = %#v, ok=%v", active, ok)
	}
}

func TestRouterSessionCommandEmptyStates(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "/list"})
	waitForReplies(t, plat, 1)
	if reply := plat.lastReply(); !strings.Contains(reply, "No sessions yet") {
		t.Fatalf("empty list reply = %q", reply)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/current"})
	waitForReplies(t, plat, 2)
	if reply := plat.lastReply(); !strings.Contains(reply, "No active session") {
		t.Fatalf("empty current reply = %q", reply)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m3", Text: "/switch missing"})
	waitForReplies(t, plat, 3)
	if reply := plat.lastReply(); !strings.Contains(reply, "not found") {
		t.Fatalf("missing switch reply = %q", reply)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m4", Text: "/rename"})
	waitForReplies(t, plat, 4)
	if reply := plat.lastReply(); !strings.Contains(reply, "Usage: /rename") {
		t.Fatalf("rename usage reply = %q", reply)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m5", Text: "/archive"})
	waitForReplies(t, plat, 5)
	if reply := plat.lastReply(); !strings.Contains(reply, "no active session") {
		t.Fatalf("archive empty reply = %q", reply)
	}
	if starts, resumes, _ := eng.counts(); starts != 0 || resumes != 0 {
		t.Fatalf("session commands invoked engine: starts=%d resumes=%d", starts, resumes)
	}
	if got := router.sessions.ActiveID(key); got != "" {
		t.Fatalf("empty commands created a session: %q", got)
	}
}

func TestRouterListAllIncludesArchived(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "one"})
	waitForReplies(t, plat, 1)
	first := router.sessions.List(key, false)[0]

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/new two"})
	waitForReplies(t, plat, 2)

	if err := router.sessions.Archive(key, first.ID); err != nil {
		t.Fatal(err)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m3", Text: "/list"})
	waitForReplies(t, plat, 3)
	if reply := plat.lastReply(); strings.Contains(reply, "one") {
		t.Fatalf("default list included archived session: %q", reply)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m4", Text: "/list all"})
	waitForReplies(t, plat, 4)
	if reply := plat.lastReply(); !strings.Contains(reply, "one") || !strings.Contains(reply, "archived") {
		t.Fatalf("list all reply = %q", reply)
	}
}

func TestRouterGroupSessionManagementRequiresAdmin(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	router.cfg.AdminUsers = []string{"admin-1"}

	group := platform.Message{
		Channel: "feishu", ChatID: "group-1", ChatType: "group",
		MessageID: "m1", UserID: "guest-1", Text: "/list",
	}
	router.Handle(group)
	waitForReplies(t, plat, 1)
	if reply := plat.lastReply(); !strings.Contains(reply, "admin users") {
		t.Fatalf("guest group reply = %q", reply)
	}

	group.UserID = "admin-1"
	group.MessageID = "m2"
	router.Handle(group)
	waitForReplies(t, plat, 2)
	if reply := plat.lastReply(); !strings.Contains(reply, "No sessions yet") {
		t.Fatalf("admin group reply = %q", reply)
	}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "dm-1", MessageID: "m3",
		UserID: "guest-1", Text: "/list",
	})
	waitForReplies(t, plat, 3)
	if reply := plat.lastReply(); !strings.Contains(reply, "No sessions yet") {
		t.Fatalf("guest DM reply = %q", reply)
	}
}

func TestRouterUnarchiveRestoresSession(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	session, err := router.sessions.Create(key, "archived")
	if err != nil {
		t.Fatal(err)
	}
	if err := router.sessions.Archive(key, session.ID); err != nil {
		t.Fatal(err)
	}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1",
		Text: "/unarchive " + session.ID,
	})
	waitForReplies(t, plat, 1)
	restored, ok := router.sessions.GetSession(session.ID)
	if !ok || restored.Status != SessionStatusActive {
		t.Fatalf("restored session = %#v, ok=%v", restored, ok)
	}
	if got := router.sessions.ActiveID(key); got != session.ID {
		t.Fatalf("active after unarchive = %q, want %q", got, session.ID)
	}
}

func TestRouterListAllNumberingSkipsArchived(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	first, err := router.sessions.Create(key, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.sessions.Create(key, "second"); err != nil {
		t.Fatal(err)
	}
	if err := router.sessions.Archive(key, first.ID); err != nil {
		t.Fatal(err)
	}

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "/list all",
	})
	waitForReplies(t, plat, 1)
	reply := plat.lastReply()
	if !strings.Contains(reply, "archived:  first") || !strings.Contains(reply, "1:  second") {
		t.Fatalf("list all numbering = %q", reply)
	}
	if strings.Contains(reply, "1:  first") {
		t.Fatalf("archived session received a switch number: %q", reply)
	}
}

func TestRouterStopCancelsNonActiveRun(t *testing.T) {
	plat := &fakePlatform{}
	eng := &contextEngine{
		startCh:  make(chan struct{}),
		cancelCh: make(chan struct{}),
	}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "long run",
	})
	select {
	case <-eng.startCh:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not start")
	}
	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/new",
	})
	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m3", Text: "/stop",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		plat.mu.Lock()
		found := false
		for _, reply := range plat.replies {
			if strings.Contains(reply, "Stop requested") {
				found = true
				break
			}
		}
		plat.mu.Unlock()
		if found {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-eng.cancelCh:
	case <-time.After(2 * time.Second):
		t.Fatal("non-active run was not cancelled")
	}
	waitForReplies(t, plat, 3)
}

func TestExecuteScheduledUsesBoundSession(t *testing.T) {
	plat := &fakePlatform{}
	eng := &fakeEngine{}
	router, _ := newTestRouter(t, eng, plat)
	key := SessionKey{Channel: "feishu", ChatID: "c1", AgentID: "codex"}
	first, err := router.sessions.Create(key, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := router.sessions.Create(key, "second")
	if err != nil {
		t.Fatal(err)
	}
	if err := router.sessions.SetEngineSession(first.ID, "engine-first"); err != nil {
		t.Fatal(err)
	}
	if err := router.sessions.SetEngineSession(second.ID, "engine-second"); err != nil {
		t.Fatal(err)
	}

	if err := router.ExecuteScheduled(context.Background(), platform.Message{
		Channel: "feishu", ChatID: "c1", Text: "scheduled",
	}, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, last := eng.counts(); last != "engine-first" {
		t.Fatalf("scheduled run resumed %q, want engine-first", last)
	}
}

func TestBuildScheduledTaskCapturesSessionID(t *testing.T) {
	task, err := buildScheduledTask(ScheduleRequest{
		Mode:      "every",
		Every:     "1h",
		Prompt:    "check",
		Channel:   "feishu",
		ChatID:    "c1",
		SessionID: "ses_bound",
	}, time.Now(), "Local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if task.SessionID != "ses_bound" {
		t.Fatalf("scheduled session id = %q", task.SessionID)
	}
}

func TestParseSessionListArgs(t *testing.T) {
	cases := []struct {
		arg       string
		all       bool
		limit     int
		wantError bool
	}{
		{arg: "", limit: 50},
		{arg: "all", all: true, limit: 50},
		{arg: "20", limit: 20},
		{arg: "all 100", all: true, limit: 100},
		{arg: "all 0", wantError: true},
		{arg: "foo", wantError: true},
	}
	for _, tc := range cases {
		all, limit, err := parseSessionListArgs(tc.arg)
		if tc.wantError {
			if err == nil {
				t.Fatalf("parseSessionListArgs(%q) succeeded", tc.arg)
			}
			continue
		}
		if err != nil || all != tc.all || limit != tc.limit {
			t.Fatalf("parseSessionListArgs(%q) = %v, %d, %v", tc.arg, all, limit, err)
		}
	}
}

type workspaceBlockingEngine struct {
	mu      sync.Mutex
	starts  int
	first   chan struct{}
	release chan struct{}
}

func (e *workspaceBlockingEngine) Name() string { return "workspace-blocking" }

func (e *workspaceBlockingEngine) Start(context.Context, string, engine.EngineOptions) (<-chan engine.Event, error) {
	e.mu.Lock()
	e.starts++
	if e.starts == 1 {
		close(e.first)
	}
	e.mu.Unlock()
	ch := make(chan engine.Event, 2)
	ch <- engine.Event{Type: engine.EventSessionStarted, SessionID: "blocking"}
	go func() {
		<-e.release
		ch <- engine.Event{Type: engine.EventMessage, Text: "done"}
		ch <- engine.Event{Type: engine.EventDone}
		close(ch)
	}()
	return ch, nil
}

func (e *workspaceBlockingEngine) Resume(context.Context, string, string, engine.EngineOptions) (<-chan engine.Event, error) {
	return e.Start(context.Background(), "", engine.EngineOptions{})
}

func (e *workspaceBlockingEngine) Cancel(context.Context, string) error { return nil }

func (e *workspaceBlockingEngine) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.starts
}

func TestRouterSerializesRunsSharingWorkspace(t *testing.T) {
	plat := &fakePlatform{}
	eng := &workspaceBlockingEngine{
		first:   make(chan struct{}),
		release: make(chan struct{}),
	}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "first",
	})
	select {
	case <-eng.first:
	case <-time.After(2 * time.Second):
		t.Fatal("first run did not start")
	}
	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "/new",
	})
	router.Handle(platform.Message{
		Channel: "feishu", ChatID: "c1", MessageID: "m3", Text: "second",
	})
	time.Sleep(100 * time.Millisecond)
	if got := eng.count(); got != 1 {
		t.Fatalf("second run started before workspace lock released: starts=%d", got)
	}
	close(eng.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && eng.count() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if got := eng.count(); got != 2 {
		t.Fatalf("second run did not start after release: starts=%d", got)
	}
	waitForReplies(t, plat, 3)
}
