package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/zhouyg1992/nectar/internal/service"
	"github.com/zhouyg1992/nectar/internal/update"
)

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("nectar update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = printUpdateUsage
	checkOnly := fs.Bool("check", false, "check for a newer release without installing it")
	force := fs.Bool("force", false, "reinstall even when already on the target version")
	targetVersion := fs.String("version", "", "release version to install, for example v1.2.3")
	noRestart := fs.Bool("no-restart", false, "update the binary without restarting the service")
	configPath := fs.String("config", "", "path to config.toml for service restart detection")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	updater := update.New(update.Config{
		CurrentVersion: version,
		Logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	})
	result, err := updater.Run(context.Background(), update.Options{
		Version:   *targetVersion,
		CheckOnly: *checkOnly,
		Force:     *force,
	})
	if err != nil {
		return err
	}

	if *checkOnly {
		fmt.Printf("Current: %s\n", result.CurrentVersion)
		fmt.Printf("Latest:  %s\n", result.TargetVersion)
		if result.UpdateAvailable {
			fmt.Println("Update available: yes")
		} else {
			fmt.Println("Update available: no")
		}
		return nil
	}
	if !result.UpdateAvailable {
		fmt.Printf("Already up to date: %s\n", result.CurrentVersion)
		return nil
	}
	if !result.Updated {
		return errors.New("update finished without installing a binary")
	}

	fmt.Printf("Updated Nectar: %s -> %s\n", result.CurrentVersion, result.TargetVersion)
	if *noRestart {
		return nil
	}

	resolvedConfig, err := resolveServiceConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not resolve config for service restart: %v\n", err)
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not locate executable for service restart: %v\n", err)
		return nil
	}
	manager, err := service.New(executable, resolvedConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not initialize service manager: %v\n", err)
		return nil
	}
	if !manager.Installed() {
		fmt.Println("Service is not installed; skipping restart.")
		return nil
	}
	if err := manager.Restart(context.Background()); err != nil {
		return fmt.Errorf("binary updated, but service restart failed; restart it manually: %w", err)
	}
	fmt.Println("Service restarted.")
	return nil
}

func printUpdateUsage() {
	fmt.Print(`Usage:
  nectar update [--check] [--force] [--version vX.Y.Z] [--no-restart] [--config PATH]

Options:
  --check          check for a newer release without installing it
  --force          reinstall even when already on the target version
  --version TAG    install a specific release (default: latest)
  --no-restart     update the binary without restarting the service
  --config PATH    config path used to detect the installed service
`)
}
