package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhouyg1992/nectar/config"
)

type UninstallOptions struct {
	Home         string
	DataDir      string
	ConfigPath   string
	NoWorkspace  bool
	Quiet        bool
	DryRun       bool
	WorkspaceDir []string
	Logf         func(string, ...any)
}

// Uninstall removes the Nectar-managed skills from all known agent roots and
// deletes only the managed blocks from workspace AGENTS.md files.
func Uninstall(opts UninstallOptions) error {
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home directory: %w", err)
		}
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(format string, args ...any) {
			if !opts.Quiet {
				fmt.Printf(format+"\n", args...)
			}
		}
	}

	dataDir := strings.TrimSpace(opts.DataDir)
	var cfg config.Config
	cfgLoaded := false
	if loaded, err := config.Load(opts.ConfigPath); err == nil {
		cfg = loaded
		cfgLoaded = true
		if dataDir == "" {
			dataDir = loaded.Storage.DataDir
		}
	} else {
		logf("warning: could not load config for skill targets: %v", err)
	}
	if dataDir == "" {
		dataDir = filepath.Join(home, ".nectar")
	}

	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	roots := uniqueDirs([]string{
		filepath.Join(dataDir, "skills"),
		filepath.Join(codexHome, "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".agents", "skills"),
	})
	for _, spec := range managedSkills {
		for _, root := range roots {
			dest := filepath.Join(root, spec.name)
			if _, err := os.Lstat(dest); os.IsNotExist(err) {
				logf("%s skill already absent → %s", spec.name, dest)
				continue
			} else if err != nil {
				return fmt.Errorf("inspect skill %s: %w", dest, err)
			}
			if opts.DryRun {
				logf("would remove %s skill → %s", spec.name, dest)
				continue
			}
			if err := os.RemoveAll(dest); err != nil {
				return fmt.Errorf("remove skill %s: %w", dest, err)
			}
			logf("removed %s skill → %s", spec.name, dest)
		}
	}

	if opts.NoWorkspace {
		return nil
	}
	workspaceDirs := opts.WorkspaceDir
	if len(workspaceDirs) == 0 {
		workspaceDirs = defaultWorkspaceDirs(home, cfg, cfgLoaded)
	}
	for _, spec := range managedSkills {
		for _, dir := range uniqueDirs(workspaceDirs) {
			if err := uninstallWorkspaceHint(dir, spec, logf, opts.DryRun); err != nil {
				return err
			}
		}
	}
	return nil
}

func uninstallWorkspaceHint(dir string, spec skillSpec, logf func(string, ...any), dryRun bool) error {
	agentsPath := filepath.Join(dir, "AGENTS.md")
	existing, err := os.ReadFile(agentsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", agentsPath, err)
	}
	content := string(existing)
	updated := spec.pattern.ReplaceAllLiteralString(content, "")
	if updated == content {
		logf("%s hint already absent → %s", spec.name, agentsPath)
		return nil
	}
	if dryRun {
		logf("would remove %s hint → %s", spec.name, agentsPath)
		return nil
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(agentsPath); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(dir, ".AGENTS-*.tmp")
	if err != nil {
		return fmt.Errorf("create AGENTS temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.WriteString(updated); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write AGENTS temp file: %w", err)
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod AGENTS temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close AGENTS temp file: %w", err)
	}
	if err := os.Rename(tempPath, agentsPath); err != nil {
		return fmt.Errorf("replace %s: %w", agentsPath, err)
	}
	cleanup = false
	logf("removed %s hint → %s", spec.name, agentsPath)
	return nil
}
