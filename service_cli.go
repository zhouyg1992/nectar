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
// environment does not inherit the shell's FEISHU_* / DINGTALK_* variables.
func validateServiceConfig(path string) error {
	var raw struct {
		Bot struct {
			Platform string `toml:"platform"`
		} `toml:"bot"`
		Feishu struct {
			AppID     string `toml:"app_id"`
			AppSecret string `toml:"app_secret"`
		} `toml:"feishu"`
		DingTalk struct {
			ClientID     string `toml:"client_id"`
			ClientSecret string `toml:"client_secret"`
			AppKey       string `toml:"app_key"`
			AppSecret    string `toml:"app_secret"`
		} `toml:"dingtalk"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return fmt.Errorf("load service config %s: %w", path, err)
	}

	platform := strings.ToLower(strings.TrimSpace(raw.Bot.Platform))
	feishuAppID := strings.TrimSpace(raw.Feishu.AppID)
	feishuSecret := strings.TrimSpace(raw.Feishu.AppSecret)
	dingTalkID := strings.TrimSpace(firstNonEmpty(raw.DingTalk.ClientID, raw.DingTalk.AppKey))
	dingTalkSecret := strings.TrimSpace(firstNonEmpty(raw.DingTalk.ClientSecret, raw.DingTalk.AppSecret))
	feishuConfigured := !isPlaceholderAppID(feishuAppID) || !isPlaceholderSecret(feishuSecret)
	dingTalkConfigured := !isPlaceholderDingTalkClientID(dingTalkID) || !isPlaceholderDingTalkSecret(dingTalkSecret)

	if platform == "" {
		if feishuConfigured && dingTalkConfigured {
			return fmt.Errorf(
				"both [feishu] and [dingtalk] credentials are configured in %s; set bot.platform to feishu or dingtalk",
				path,
			)
		}
		if dingTalkConfigured {
			platform = "dingtalk"
		} else {
			platform = "feishu"
		}
	}

	switch platform {
	case "feishu":
		if isPlaceholderAppID(feishuAppID) || isPlaceholderSecret(feishuSecret) {
			return fmt.Errorf(
				"feishu app_id/app_secret are not configured in %s; edit the file first. Service mode reads credentials from this file, not from environment variables",
				path,
			)
		}
	case "dingtalk":
		if isPlaceholderDingTalkClientID(dingTalkID) || isPlaceholderDingTalkSecret(dingTalkSecret) {
			return fmt.Errorf(
				"dingtalk client_id/client_secret are not configured in %s; edit the file first. Service mode reads credentials from this file, not from environment variables",
				path,
			)
		}
	default:
		return fmt.Errorf("unsupported bot.platform %q in %s (supported: feishu, dingtalk)", platform, path)
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

func isPlaceholderAppID(value string) bool {
	return value == "" || value == "cli_xxx" || strings.HasPrefix(value, "cli_x")
}

func isPlaceholderSecret(value string) bool {
	return value == "" || value == "xxx" || (len(value) >= 8 && strings.Trim(value, "x") == "")
}

func isPlaceholderDingTalkClientID(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.Contains(value, "xxxxxxxx") || (len(value) >= 8 && strings.Trim(value, "x") == "")
}

func isPlaceholderDingTalkSecret(value string) bool {
	value = strings.TrimSpace(value)
	return isPlaceholderSecret(value) || strings.Contains(value, "xxxxxxxx")
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
