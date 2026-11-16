package execguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceWorktreeIsolatesSessions(t *testing.T) {
	repo := gitRepo(t)
	dataDir := t.TempDir()
	workspace, err := NewWorkspaceWithOptions(Options{
		Root:    repo,
		Mode:    ModeWorktree,
		DataDir: dataDir,
		BotID:   "codex_dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Mode() != ModeWorktree {
		t.Fatalf("mode = %q", workspace.Mode())
	}
	session, err := workspace.ForSession("ses_abc", "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(session.Path, filepath.Join(dataDir, "worktrees", "codex_dev")) {
		t.Fatalf("worktree path = %q", session.Path)
	}
	if _, err := os.Stat(filepath.Join(session.Path, ".git")); err != nil {
		t.Fatalf("worktree .git missing: %v", err)
	}
	branch := strings.TrimSpace(runGit(t, session.Path, "rev-parse", "--abbrev-ref", "HEAD"))
	if branch != "nectar/codex_dev/ses_abc" {
		t.Fatalf("branch = %q", branch)
	}
	wantRepo := repo
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		wantRepo = resolved
	}
	if session.ProjectRoot != wantRepo {
		t.Fatalf("project root = %q, want %q", session.ProjectRoot, wantRepo)
	}
	if err := os.WriteFile(filepath.Join(session.Path, "worktree-only.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "worktree-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("main worktree saw session file: %v", err)
	}
	again, err := workspace.ForSession("ses_abc", "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Path != session.Path {
		t.Fatalf("worktree not reused: %q vs %q", again.Path, session.Path)
	}
	if got := workspace.MemoryRoot("chat-1"); got != wantRepo {
		t.Fatalf("memory root = %q, want %q", got, wantRepo)
	}
	items, err := workspace.ListWorktrees()
	if err != nil {
		t.Fatal(err)
	}
	wantPath := session.Path
	if resolved, err := filepath.EvalSymlinks(session.Path); err == nil {
		wantPath = resolved
	}
	if len(items) != 1 || items[0].Path != wantPath {
		t.Fatalf("worktrees = %#v", items)
	}
	if err := workspace.RemoveWorktree("ses_abc", true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
	items, err = workspace.ListWorktrees()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("worktrees after remove = %#v", items)
	}
}

func TestWorkspaceAutoFallsBackForNonGitRoot(t *testing.T) {
	root := t.TempDir()
	workspace, err := NewWorkspaceWithOptions(Options{Root: root, Mode: ModeAuto, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Mode() != ModePerChat {
		t.Fatalf("mode = %q, want per_chat", workspace.Mode())
	}
	session, err := workspace.ForSession("ses_1", "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Path != filepath.Join(root, "chat-1") {
		t.Fatalf("session path = %q", session.Path)
	}
}

func TestWorkspaceExplicitWorktreeRequiresGit(t *testing.T) {
	_, err := NewWorkspaceWithOptions(Options{
		Root:    t.TempDir(),
		Mode:    ModeWorktree,
		DataDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected worktree requirement error")
	}
}

func TestWorkspaceRemoveAllWorktreesRequiresForceWhenDirty(t *testing.T) {
	repo := gitRepo(t)
	workspace, err := NewWorkspaceWithOptions(Options{
		Root:    repo,
		Mode:    ModeWorktree,
		DataDir: t.TempDir(),
		BotID:   "bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := workspace.ForSession("ses_dirty", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session.Path, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := workspace.RemoveAllWorktrees(false, false); err == nil {
		t.Fatal("expected dirty worktree removal to fail without force")
	}
	if err := workspace.RemoveAllWorktrees(true, false); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceRemoveWorktreeCanDeleteBranch(t *testing.T) {
	repo := gitRepo(t)
	workspace, err := NewWorkspaceWithOptions(Options{
		Root:    repo,
		Mode:    ModeWorktree,
		DataDir: t.TempDir(),
		BotID:   "bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.ForSession("ses_branch", "chat"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.RemoveWorktree("ses_branch", true, true); err != nil {
		t.Fatal(err)
	}
	if branches := strings.TrimSpace(runGit(t, repo, "branch", "--list", "nectar/bot/ses_branch")); branches != "" {
		t.Fatalf("branch still exists: %q", branches)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
