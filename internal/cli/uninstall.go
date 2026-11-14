package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/service"
	"github.com/zhouyg1992/nectar/internal/skills"
)

func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	binaryPath := fs.String("binary", "", "path to the installed nectar binary")
	purgeData := fs.Bool("purge-data", false, "also remove config, data, workspaces, and skill backups")
	keepBinary := fs.Bool("keep-binary", false, "keep the nectar binary")
	noSkills := fs.Bool("no-skills", false, "keep installed agent skills and workspace hints")
	dryRun := fs.Bool("dry-run", false, "show what would be removed")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	resolvedConfig, err := resolveServiceConfig(*configPath)
	if err != nil {
		return err
	}
	resolvedBinary, err := resolveServiceBinary(*binaryPath)
	if err != nil {
		return err
	}

	dataDir, workspaceDirs, cfgErr := uninstallPaths(home, resolvedConfig)
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load config for complete cleanup: %v\n", cfgErr)
	}

	if *dryRun {
		fmt.Println("would uninstall background service")
		if !*noSkills {
			fmt.Println("would uninstall agent skills and workspace hints")
		}
		if !*keepBinary {
			fmt.Println("would remove", resolvedBinary)
		}
		if *purgeData {
			fmt.Println("would remove", dataDir)
			if !pathWithin(resolvedConfig, dataDir) {
				fmt.Println("would remove", resolvedConfig)
			}
		} else {
			fmt.Println("would keep config and data in", dataDir)
		}
		return nil
	}

	var cleanupErrs []error
	ctx := context.Background()
	if manager, err := service.New(resolvedBinary, resolvedConfig); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("prepare service uninstall: %w", err))
	} else if err := manager.Uninstall(ctx); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}

	if !*noSkills {
		if err := skills.Uninstall(skills.UninstallOptions{
			Home:         home,
			DataDir:      dataDir,
			ConfigPath:   resolvedConfig,
			WorkspaceDir: workspaceDirs,
		}); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
	}

	if !*keepBinary {
		if err := os.Remove(resolvedBinary); err != nil && !os.IsNotExist(err) {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("remove binary %s: %w", resolvedBinary, err))
		} else if err == nil {
			fmt.Println("removed", resolvedBinary)
		}
	}

	if *purgeData {
		if err := removeManagedDataDir(dataDir, home); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
		if !pathWithin(resolvedConfig, dataDir) {
			if err := os.Remove(resolvedConfig); err != nil && !os.IsNotExist(err) {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("remove config %s: %w", resolvedConfig, err))
			}
		}
	} else {
		fmt.Println("kept config and data in", dataDir)
	}

	if len(cleanupErrs) > 0 {
		return errors.Join(cleanupErrs...)
	}
	fmt.Println("Nectar uninstalled.")
	return nil
}

func pathWithin(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func uninstallPaths(home, configPath string) (string, []string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		var partial config.Config
		if _, decodeErr := toml.DecodeFile(configPath, &partial); decodeErr != nil {
			return filepath.Join(home, ".nectar"), nil, err
		}
		cfg = partial
	}
	dataDir := service.ExpandPath(cfg.Storage.DataDir)
	if value := strings.TrimSpace(os.Getenv("NECTAR_DATA_DIR")); value != "" {
		dataDir = service.ExpandPath(value)
	}
	if dataDir == "" {
		dataDir = filepath.Join(home, ".nectar")
	}
	workingDirs := []string{
		filepath.Join(home, ".nectar", "workspace"),
		service.ExpandPath(cfg.Codex.WorkingDir),
		service.ExpandPath(cfg.OpenCode.WorkingDir),
		service.ExpandPath(cfg.Cursor.WorkingDir),
		service.ExpandPath(cfg.Claude.WorkingDir),
		service.ExpandPath(cfg.Pi.WorkingDir),
	}
	if value := strings.TrimSpace(os.Getenv("NECTAR_WORKING_DIR")); value != "" {
		workingDirs = append(workingDirs, service.ExpandPath(value))
	}
	return dataDir, workingDirs, nil
}

func removeManagedDataDir(dataDir, home string) error {
	absolute, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}
	absolute = filepath.Clean(absolute)
	volumeRoot := filepath.Clean(filepath.VolumeName(absolute) + string(filepath.Separator))
	cleanHome := filepath.Clean(home)
	if absolute == volumeRoot || absolute == cleanHome || absolute == filepath.Dir(cleanHome) {
		return fmt.Errorf("refusing to purge unsafe data directory %s", absolute)
	}
	if err := os.RemoveAll(absolute); err != nil {
		return fmt.Errorf("remove data directory %s: %w", absolute, err)
	}
	fmt.Println("removed", absolute)
	return nil
}
