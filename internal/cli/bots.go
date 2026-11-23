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

func runBots(args []string) error {
	fs := flag.NewFlagSet("bots", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config.toml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		return err
	}
	return printConfiguredBots(bots)
}

func printConfiguredBots(bots []config.ResolvedBot) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPLATFORM\tENGINE\tWORKSPACE\tMODE\tDATA DIR")
	for _, bot := range bots {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			bot.ID,
			bot.Name,
			bot.Config.Bot.Platform,
			bot.Config.Bot.Engine,
			workspaceForEngine(bot.Config),
			bot.Config.Bot.WorkspaceMode,
			bot.Config.Storage.DataDir,
		)
	}
	return w.Flush()
}

func loadBotTarget(configPath, botID string) (config.ResolvedBot, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return config.ResolvedBot{}, err
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		return config.ResolvedBot{}, err
	}
	if len(bots) == 0 {
		return config.ResolvedBot{}, fmt.Errorf("no bots configured")
	}
	if strings.TrimSpace(botID) != "" {
		return findBot(bots, botID)
	}
	if len(bots) == 1 {
		return bots[0], nil
	}
	for _, bot := range bots {
		if bot.ID == "default" {
			return bot, nil
		}
	}
	return config.ResolvedBot{}, fmt.Errorf("multiple bots configured; use --bot <id> (available: %s)", botIDs(bots))
}

func loadConfigForBot(configPath, botID string) (config.Config, error) {
	if strings.TrimSpace(botID) == "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			return config.Config{}, err
		}
		if len(cfg.Bots) == 0 {
			return cfg, nil
		}
		bots, err := cfg.ResolveBots()
		if err != nil {
			return config.Config{}, err
		}
		if len(bots) == 1 {
			return bots[0].Config, nil
		}
		for _, bot := range bots {
			if bot.ID == "default" {
				return bot.Config, nil
			}
		}
		return config.Config{}, fmt.Errorf("multiple bots configured; use --bot <id> (available: %s)", botIDs(bots))
	}
	bot, err := loadBotTarget(configPath, botID)
	if err != nil {
		return config.Config{}, err
	}
	return bot.Config, nil
}

func findBot(bots []config.ResolvedBot, ref string) (config.ResolvedBot, error) {
	ref = strings.TrimSpace(ref)
	for _, bot := range bots {
		if bot.ID == ref || strings.EqualFold(bot.Name, ref) {
			return bot, nil
		}
	}
	return config.ResolvedBot{}, fmt.Errorf("bot %q not found (available: %s)", ref, botIDs(bots))
}

func botIDs(bots []config.ResolvedBot) string {
	ids := make([]string, 0, len(bots))
	for _, bot := range bots {
		ids = append(ids, bot.ID)
	}
	return strings.Join(ids, ", ")
}

func botAPISocket(bot config.ResolvedBot) string {
	return filepath.Join(bot.Config.Storage.DataDir, "run", "api.sock")
}

func resolveScheduleSocket(explicitSocket, configPath, botID string) (string, config.ResolvedBot, bool, error) {
	if socket := strings.TrimSpace(explicitSocket); socket != "" {
		return socket, config.ResolvedBot{}, false, nil
	}
	if strings.TrimSpace(configPath) != "" || strings.TrimSpace(botID) != "" {
		bot, err := loadBotTarget(configPath, botID)
		if err != nil {
			return "", config.ResolvedBot{}, false, err
		}
		return botAPISocket(bot), bot, true, nil
	}
	if socket := strings.TrimSpace(os.Getenv("NECTAR_API_SOCKET")); socket != "" {
		return socket, config.ResolvedBot{}, false, nil
	}
	if cfg, err := config.Load(""); err == nil && len(cfg.Bots) > 0 {
		bot, err := loadBotTarget("", "")
		if err != nil {
			return "", config.ResolvedBot{}, false, err
		}
		return botAPISocket(bot), bot, true, nil
	}
	return scheduleSocketPath(), config.ResolvedBot{}, false, nil
}

func workspaceForEngine(cfg config.Config) string {
	switch cfg.Bot.Engine {
	case "opencode":
		return cfg.OpenCode.WorkingDir
	case "cursor":
		return cfg.Cursor.WorkingDir
	case "claude":
		return cfg.Claude.WorkingDir
	case "pi":
		return cfg.Pi.WorkingDir
	case "antigravity":
		return cfg.Antigravity.WorkingDir
	default:
		return cfg.Codex.WorkingDir
	}
}

func workspaceForBot(bot config.ResolvedBot) (*execguard.Workspace, error) {
	return execguard.NewWorkspaceWithOptions(execguard.Options{
		Root:    workspaceForEngine(bot.Config),
		Mode:    execguard.Mode(bot.Config.Bot.WorkspaceMode),
		DataDir: bot.WorktreeDir,
		BotID:   bot.ID,
	})
}
