package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhouyg1992/nectar/internal/core"
	"github.com/zhouyg1992/nectar/internal/memory"
)

func TestMemoryCLIAddSearchAndForget(t *testing.T) {
	configPath, dataDir := writeMemoryCLIConfig(t, "")
	t.Setenv("NECTAR_CHANNEL", "feishu")
	t.Setenv("NECTAR_CHAT_ID", "c1")
	t.Setenv("NECTAR_USER_ID", "user-1")
	t.Setenv("NECTAR_WORKING_DIR", t.TempDir())

	if err := runMemory([]string{
		"add", "--config", configPath, "--kind", "profile", "--status", "active",
		"以后默认用中文回复",
	}); err != nil {
		t.Fatal(err)
	}

	store, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Scope != memory.ScopeChat {
		t.Fatalf("active memories = %#v", active)
	}
	if active[0].ScopeID != memory.ChatScope("feishu", "c1") {
		t.Fatalf("scope id = %q", active[0].ScopeID)
	}

	out := captureMemoryCLIOutput(t, func() error {
		return runMemory([]string{"list", "--config", configPath, "--json"})
	})
	if !strings.Contains(out, active[0].ID) {
		t.Fatalf("default list did not include active memory: %s", out)
	}
	if err := runMemory([]string{"search", "--config", configPath, "中文"}); err != nil {
		t.Fatal(err)
	}
	if err := runMemory([]string{"get", "--config", configPath, active[0].ID}); err != nil {
		t.Fatal(err)
	}

	if err := runMemory([]string{"forget", "--config", configPath, active[0].ID}); err == nil {
		t.Fatal("forget without --confirm should fail")
	}
	if _, ok, err := store.Get(context.Background(), active[0].ID); err != nil || !ok {
		t.Fatalf("memory missing after rejected forget: ok=%v err=%v", ok, err)
	}
	if err := runMemory([]string{"forget", "--config", configPath, active[0].ID, "--confirm"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.Get(context.Background(), active[0].ID); err != nil || ok {
		t.Fatalf("memory still present: ok=%v err=%v", ok, err)
	}
}

func TestMemoryCLIProjectScope(t *testing.T) {
	configPath, dataDir := writeMemoryCLIConfig(t, "")
	project := filepath.Join(t.TempDir(), "nectar")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := runMemory([]string{
		"add", "--config", configPath, "--scope", "project", "--project", project,
		"--status", "active", "这个项目使用 Go 1.22",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Scope != memory.ScopeProject {
		t.Fatalf("project memories = %#v", items)
	}
	if items[0].ScopeID != "project:nectar" {
		t.Fatalf("project scope id = %q", items[0].ScopeID)
	}
}

func TestMemoryCLIGlobalRequiresAdmin(t *testing.T) {
	configPath, _ := writeMemoryCLIConfig(t, `["admin-1"]`)
	t.Setenv("NECTAR_USER_ID", "guest-1")

	if err := runMemory([]string{
		"add", "--config", configPath, "--scope", "global", "--status", "active",
		"全局偏好",
	}); err == nil {
		t.Fatal("guest should not write global memory")
	}

	t.Setenv("NECTAR_USER_ID", "admin-1")
	if err := runMemory([]string{
		"add", "--config", configPath, "--scope", "global", "--status", "active",
		"全局偏好",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryCLIListJSONAndPendingAlias(t *testing.T) {
	configPath, dataDir := writeMemoryCLIConfig(t, "")
	t.Setenv("NECTAR_CHANNEL", "feishu")
	t.Setenv("NECTAR_CHAT_ID", "c1")
	t.Setenv("NECTAR_WORKING_DIR", t.TempDir())

	if err := runMemory([]string{
		"add", "--config", configPath, "--status", "candidate", "待审核记忆",
	}); err != nil {
		t.Fatal(err)
	}
	if err := runMemory([]string{"list", "--config", configPath, "--status", "pending", "--json"}); err != nil {
		t.Fatal(err)
	}
	store, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.List(context.Background(), memory.ListOptions{Status: memory.StatusCandidate})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
}

func TestMemoryCLIUpdateContent(t *testing.T) {
	configPath, dataDir := writeMemoryCLIConfig(t, "")
	t.Setenv("NECTAR_CHANNEL", "feishu")
	t.Setenv("NECTAR_CHAT_ID", "c1")

	if err := runMemory([]string{
		"add", "--config", configPath, "--status", "active", "旧内容",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %#v err=%v", items, err)
	}
	if err := runMemory([]string{
		"update", "--config", configPath, items[0].ID, "新内容",
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	updated, ok, err := reopened.Get(context.Background(), items[0].ID)
	if err != nil || !ok {
		t.Fatalf("updated memory missing: ok=%v err=%v", ok, err)
	}
	if updated.Content != "新内容" || updated.Revision <= items[0].Revision {
		t.Fatalf("updated = %#v, old revision=%d", updated, items[0].Revision)
	}
}

func TestMemoryCLIGroupRequiresAdminForActive(t *testing.T) {
	configPath, dataDir := writeMemoryCLIConfig(t, `["admin-1"]`)
	t.Setenv("NECTAR_CHANNEL", "feishu")
	t.Setenv("NECTAR_CHAT_ID", "group-1")
	t.Setenv("NECTAR_CHAT_TYPE", "group")
	t.Setenv("NECTAR_USER_ID", "guest-1")

	if err := runMemory([]string{
		"add", "--config", configPath, "--status", "active", "群里的约定",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.List(context.Background(), memory.ListOptions{Status: memory.StatusCandidate})
	if err != nil || len(pending) != 1 {
		t.Fatalf("group add should be candidate: %#v err=%v", pending, err)
	}
	if err := runMemory([]string{"approve", "--config", configPath, pending[0].ID}); err == nil {
		t.Fatal("guest should not approve group memory")
	}
	t.Setenv("NECTAR_USER_ID", "admin-1")
	if err := runMemory([]string{"approve", "--config", configPath, pending[0].ID}); err != nil {
		t.Fatal(err)
	}
	active, err := store.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
	if err != nil || len(active) != 1 {
		t.Fatalf("admin approval failed: %#v err=%v", active, err)
	}
}

func TestMemoryCLIUsesDaemonSocket(t *testing.T) {
	configPath, dataDir := writeMemoryCLIConfig(t, "")
	store, err := memory.NewFileStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := core.NewScheduleAPI(core.ScheduleAPIOptions{
		SocketPath: socket,
		Memory:     store,
	})
	if err := api.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	t.Setenv("NECTAR_API_SOCKET", socket)
	t.Setenv("NECTAR_CHANNEL", "feishu")
	t.Setenv("NECTAR_CHAT_ID", "c1")

	if err := runMemory([]string{
		"add", "--config", configPath, "--status", "active", "通过 daemon 写入",
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		items, err := store.List(context.Background(), memory.ListOptions{Status: memory.StatusActive})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("memory CLI did not write through the daemon socket")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func captureMemoryCLIOutput(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = old }()
	runErr := fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	return string(data)
}

func writeMemoryCLIConfig(t *testing.T, admins string) (string, string) {
	t.Helper()
	t.Setenv("NECTAR_API_SOCKET", "")
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	configPath := filepath.Join(root, "config.toml")
	body := fmt.Sprintf("[storage]\ndata_dir = %q\n\n[bot]\nengine = \"codex\"\n", dataDir)
	if admins != "" {
		body += "admin_users = " + admins + "\n"
	}
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, dataDir
}
