package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/zhouyg1992/nectar/internal/skills"
)

func runSkills(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printSkillsUsage()
		return nil
	}
	switch args[0] {
	case "install":
		return runSkillsInstall(args[1:])
	default:
		printSkillsUsage()
		return fmt.Errorf("unknown skills command %q", args[0])
	}
}

func runSkillsInstall(args []string) error {
	fs := flag.NewFlagSet("skills install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	dataDir := fs.String("data-dir", "", "Nectar data directory")
	noWorkspace := fs.Bool("no-workspace", false, "do not update workspace AGENTS.md files")
	quiet := fs.Bool("quiet", false, "suppress non-error output")
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
	if err := skills.Install(skills.Options{
		Home:        home,
		DataDir:     *dataDir,
		ConfigPath:  *configPath,
		NoWorkspace: *noWorkspace,
		Quiet:       *quiet,
	}); err != nil {
		return err
	}
	if !*quiet {
		fmt.Println("Agent skill installed.")
	}
	return nil
}

// refreshAgentSkills runs the newly installed binary so the embedded skill
// always matches the binary version rather than the process doing the update.
func refreshAgentSkills(binaryPath, configPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"skills", "install", "--quiet"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func printSkillsUsage() {
	fmt.Print(`Usage:
  nectar skills install [--config PATH] [--data-dir DIR] [--no-workspace] [--quiet]

Installs the Nectar agent skill for Codex, Claude, and shared Agents roots, and
updates the managed schedule block in workspace AGENTS.md files.
`)
}
