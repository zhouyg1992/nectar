package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/zhouyg1992/nectar/config"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
)

func runWorktrees(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printWorktreesUsage()
		return nil
	}
	switch args[0] {
	case "list", "ls":
		return worktreesList(args[1:])
	case "remove", "rm":
		return worktreesRemove(args[1:])
	default:
		printWorktreesUsage()
		return fmt.Errorf("unknown worktrees command %q", args[0])
	}
}

func worktreesList(args []string) error {
	fs := flag.NewFlagSet("worktrees list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	bots, err := worktreeBots(*configPath, *botID)
	if err != nil {
		return err
	}
	type row struct {
		Bot    string `json:"bot"`
		Path   string `json:"path"`
		Branch string `json:"branch"`
		Head   string `json:"head"`
	}
	var rows []row
	for _, bot := range bots {
		root := workspaceForEngine(bot.Config)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		workspace, err := workspaceForBot(bot)
		if err != nil {
			return fmt.Errorf("bot %s: %w", bot.ID, err)
		}
		if workspace.Mode() != execguard.ModeWorktree {
			continue
		}
		items, err := workspace.ListWorktrees()
		if err != nil {
			return fmt.Errorf("bot %s: %w", bot.ID, err)
		}
		for _, item := range items {
			rows = append(rows, row{Bot: bot.ID, Path: item.Path, Branch: item.Branch, Head: item.Head})
		}
	}
	if *jsonOut {
		return printJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Println("No worktrees.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "BOT\tBRANCH\tHEAD\tPATH")
	for _, item := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.Bot, item.Branch, item.Head, item.Path)
	}
	return w.Flush()
}

func worktreesRemove(args []string) error {
	target, flagArgs := splitWorktreeTarget(args)
	fs := flag.NewFlagSet("worktrees remove", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	botID := fs.String("bot", "", "bot id")
	force := fs.Bool("force", false, "remove even when the worktree has changes")
	all := fs.Bool("all", false, "remove all worktrees for the selected bot")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	bot, err := loadBotTarget(*configPath, *botID)
	if err != nil {
		return err
	}
	workspace, err := workspaceForBot(bot)
	if err != nil {
		return err
	}
	if workspace.Mode() != execguard.ModeWorktree {
		return fmt.Errorf("bot %s workspace mode is %s, not worktree", bot.ID, workspace.Mode())
	}
	if *all {
		if err := workspace.RemoveAllWorktrees(*force); err != nil {
			return err
		}
		fmt.Printf("Removed all worktrees for bot %s\n", bot.ID)
		return nil
	}
	if target == "" {
		return fmt.Errorf("worktrees remove requires a session id or worktree path")
	}
	target = strings.TrimSpace(target)
	if strings.ContainsRune(target, filepath.Separator) {
		target = filepath.Base(filepath.Clean(target))
	}
	if err := workspace.RemoveWorktree(target, *force); err != nil {
		return err
	}
	fmt.Printf("Removed worktree %s for bot %s\n", target, bot.ID)
	return nil
}

func splitWorktreeTarget(args []string) (string, []string) {
	var target string
	var flags []string
	expectValue := false
	for _, arg := range args {
		if expectValue {
			flags = append(flags, arg)
			expectValue = false
			continue
		}
		switch arg {
		case "--config", "--bot":
			flags = append(flags, arg)
			expectValue = true
		default:
			if strings.HasPrefix(arg, "-") {
				flags = append(flags, arg)
			} else if target == "" {
				target = arg
			} else {
				flags = append(flags, arg)
			}
		}
	}
	return target, flags
}

func worktreeBots(configPath, botID string) ([]config.ResolvedBot, error) {
	if strings.TrimSpace(botID) != "" {
		bot, err := loadBotTarget(configPath, botID)
		if err != nil {
			return nil, err
		}
		return []config.ResolvedBot{bot}, nil
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	return cfg.ResolveBots()
}

func printWorktreesUsage() {
	fmt.Print(`Usage:
  nectar worktrees list [--config PATH] [--bot ID] [--json]
  nectar worktrees remove <session-id|path> [--config PATH] [--bot ID] [--force]
  nectar worktrees remove --all [--config PATH] [--bot ID] [--force]

Worktrees are created per session when workspace_mode is worktree or auto on a
committed git repository. Removing a worktree does not delete its branch.
`)
}
