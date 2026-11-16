package opencode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zhouyg1992/nectar/internal/engine"
)

func TestSteerRequiresSharedServer(t *testing.T) {
	adapter := NewAdapter(Config{Executable: "opencode", WorkingDir: t.TempDir(), Standalone: true})
	if adapter.SupportsSteer() {
		t.Fatal("standalone-only adapter should not support steering")
	}
	if err := adapter.Steer(context.Background(), "s1", "hello"); err == nil || !strings.Contains(err.Error(), "standalone") {
		t.Fatalf("steer error = %v", err)
	}
}

func TestSteerCallsSharedServerAPI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	script := filepath.Join(dir, "opencode")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$OPENCODE_ARGS_FILE\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_ARGS_FILE", argsFile)

	adapter := NewAdapter(Config{
		Executable: script,
		WorkingDir: dir,
		Standalone: false,
		ServerURL:  "http://127.0.0.1:4096",
	})
	if !adapter.SupportsSteer() {
		t.Fatal("shared-server adapter should support steering")
	}
	if err := adapter.Steer(context.Background(), "sess-1", "also test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := string(data)
	for _, want := range []string{
		"api\n",
		"--server\nhttp://127.0.0.1:4096\n",
		"POST\n/api/session/sess-1/prompt\n",
		`{"delivery":"steer","text":"also test"}`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q:\n%s", want, args)
		}
	}
}

var _ engine.Steerer = (*Adapter)(nil)
