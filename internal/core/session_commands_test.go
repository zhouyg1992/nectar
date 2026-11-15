package core

import (
	"strings"
	"testing"

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
