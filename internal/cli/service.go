package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/zhouyg1992/nectar/config"
	"github.com/zhouyg1992/nectar/internal/service"
)

func runService(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printServiceUsage()
		return nil
	}

	subcommand := args[0]
	switch subcommand {
	case "install", "uninstall", "start", "stop", "restart", "status", "logs":
	default:
		return fmt.Errorf("unknown service command %q", subcommand)
	}

	fs := flag.NewFlagSet("nectar service "+subcommand, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printServiceUsage
	configPath := fs.String("config", "", "path to config.toml")
	binaryPath := fs.String("binary", "", "path to the nectar binary")
	linger := fs.Bool("linger", false, "enable systemd user linger (Linux)")
	noStart := fs.Bool("no-start", false, "install the service without starting it now")
	lines := fs.Int("n", 200, "number of log lines")
	follow := fs.Bool("f", false, "follow logs")
	botFilter := fs.String("bot", "", "show logs for one bot")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *linger && runtime.GOOS != "linux" {
		return errors.New("--linger is only supported on Linux")
	}

	resolvedConfig, err := resolveServiceConfig(*configPath)
	if err != nil {
		return err
	}
	resolvedBinary, err := resolveServiceBinary(*binaryPath)
	if err != nil {
		return err
	}
	manager, err := service.New(resolvedBinary, resolvedConfig)
	if err != nil {
		return err
	}

	ctx := context.Background()
	switch subcommand {
	case "install":
		if err := validateServiceConfig(resolvedConfig); err != nil {
			return err
		}
		return manager.Install(ctx, service.InstallOptions{
			Linger: *linger,
			Start:  !*noStart,
		})
	case "uninstall":
		return manager.Uninstall(ctx)
	case "start":
		return manager.Start(ctx)
	case "stop":
		return manager.Stop(ctx)
	case "restart":
		return manager.Restart(ctx)
	case "status":
		if err := manager.Status(ctx); err != nil {
			return err
		}
		cfg, err := config.LoadFile(resolvedConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not load configured bots: %v\n", err)
			return nil
		}
		bots, err := cfg.ResolveBots()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not resolve configured bots: %v\n", err)
			return nil
		}
		if len(bots) > 0 {
			fmt.Println()
			return printConfiguredBots(bots)
		}
		return nil
	case "logs":
		filters, err := serviceLogFilters(resolvedConfig, *botFilter)
		if err != nil {
			return err
		}
		return manager.Logs(ctx, *lines, *follow, filters)
	default:
		return fmt.Errorf("unknown service command %q", subcommand)
	}
}

func serviceLogFilters(configPath, botID string) ([]string, error) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return nil, nil
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if len(cfg.Bots) == 0 && botID == "default" {
		return nil, nil
	}
	bot, err := loadBotTarget(configPath, botID)
	if err != nil {
		return nil, err
	}
	return []string{
		"bot=" + bot.ID,
		`bot="` + bot.ID + `"`,
		"bot=" + bot.Name,
		`bot="` + bot.Name + `"`,
	}, nil
}

func resolveServiceConfig(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = config.DefaultPath()
	}
	return service.AbsolutePath(path)
}

func resolveServiceBinary(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("locate nectar binary: %w", err)
		}
		path = executable
	}
	return service.AbsolutePath(path)
}

// Service mode reads credentials from the config file because the service
// environment does not inherit the shell's FEISHU_* / DINGTALK_* / TELEGRAM_*
// variables.
func validateServiceConfig(path string) error {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return fmt.Errorf("load service config %s: %w", path, err)
	}
	bots, err := cfg.ResolveBots()
	if err != nil {
		return fmt.Errorf("service config %s: %w", path, err)
	}
	if len(bots) == 0 {
		return fmt.Errorf("service config %s has no usable bots", path)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func printServiceUsage() {
	fmt.Print(`Usage:
  nectar service install [--config PATH] [--binary PATH] [--linger] [--no-start]
  nectar service uninstall
  nectar service start
  nectar service stop
  nectar service restart
  nectar service status
  nectar service logs [-n 200] [-f] [--bot ID]

Options:
  --config PATH   path to config.toml (default ~/.nectar/config.toml)
  --binary PATH   path to the nectar binary (default current executable)
  --linger        enable systemd user linger; starts at boot without login (Linux only)
  --no-start      install and enable the service without starting it now
  -n N            number of log lines (default 200)
  -f              follow logs
  --bot ID        show logs for one bot
`)
}
