package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

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
		return manager.Status(ctx)
	case "logs":
		return manager.Logs(ctx, *lines, *follow)
	default:
		return fmt.Errorf("unknown service command %q", subcommand)
	}
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
// environment does not inherit the shell's FEISHU_APP_ID/FEISHU_APP_SECRET.
func validateServiceConfig(path string) error {
	var raw struct {
		Feishu struct {
			AppID     string `toml:"app_id"`
			AppSecret string `toml:"app_secret"`
		} `toml:"feishu"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return fmt.Errorf("load service config %s: %w", path, err)
	}
	appID := strings.TrimSpace(raw.Feishu.AppID)
	appSecret := strings.TrimSpace(raw.Feishu.AppSecret)
	if isPlaceholderAppID(appID) || isPlaceholderSecret(appSecret) {
		return fmt.Errorf(
			"feishu app_id/app_secret are not configured in %s; edit the file first. Service mode reads credentials from this file, not from environment variables",
			path,
		)
	}
	return nil
}

func isPlaceholderAppID(value string) bool {
	return value == "" || value == "cli_xxx" || strings.HasPrefix(value, "cli_x")
}

func isPlaceholderSecret(value string) bool {
	return value == "" || value == "xxx" || (len(value) >= 8 && strings.Trim(value, "x") == "")
}

func printServiceUsage() {
	fmt.Print(`Usage:
  nectar service install [--config PATH] [--binary PATH] [--linger] [--no-start]
  nectar service uninstall
  nectar service start
  nectar service stop
  nectar service restart
  nectar service status
  nectar service logs [-n 200] [-f]

Options:
  --config PATH   path to config.toml (default ~/.nectar/config.toml)
  --binary PATH   path to the nectar binary (default current executable)
  --linger        enable systemd user linger; starts at boot without login (Linux only)
  --no-start      install and enable the service without starting it now
  -n N            number of log lines (default 200)
  -f              follow logs
`)
}
