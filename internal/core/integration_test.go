package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine/codex"
	"github.com/zhouyg1992/nectar/internal/platform"
)

func TestRouterWithCodexAdapter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-codex.sh")
	body := `#!/bin/sh
case "$*" in
  *resume*) prefix=resumed ;;
  *) prefix=started ;;
esac
read line
printf '{"type":"thread.started","thread_id":"t-fake"}\n'
printf '{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"%s %s"}}\n' "$prefix" "$line"
printf '{"type":"turn.completed"}\n'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	eng := codex.NewAdapter(codex.Config{
		Executable: script,
		Timeout:    5 * time.Second,
	})
	plat := &fakePlatform{}
	router, _ := newTestRouter(t, eng, plat)

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m1", Text: "hello"})
	waitForReplies(t, plat, 1)
	if got := plat.lastReply(); got != "started hello" {
		t.Fatalf("first reply = %q", got)
	}

	router.Handle(platform.Message{Channel: "feishu", ChatID: "c1", MessageID: "m2", Text: "again"})
	waitForReplies(t, plat, 2)
	if got := plat.lastReply(); got != "resumed again" {
		t.Fatalf("second reply = %q", got)
	}
}
