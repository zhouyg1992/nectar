package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

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
	noSkills := fs.Bool("no-skills", false, "update the binary without refreshing the agent skill")
	configPath := fs.String("config", "", "path to config.toml for service restart detection")
	binaryPath := fs.String("binary", "", "path to the binary to update")
	downloadTimeout := fs.Duration("timeout", 10*time.Minute, "timeout for each download attempt")
	attempts := fs.Int("attempts", 2, "maximum attempts for metadata and asset requests")
	noProgress := fs.Bool("no-progress", false, "disable download progress output")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *downloadTimeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if *attempts <= 0 {
		return errors.New("--attempts must be positive")
	}

	cfg := update.Config{
		CurrentVersion:  version,
		ExecutablePath:  *binaryPath,
		DownloadTimeout: *downloadTimeout,
		Attempts:        *attempts,
		Logf: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
	}
	if !*noProgress {
		progress := newUpdateProgressPrinter(os.Stdout)
		cfg.Progress = progress.Report
	}
	if !*noSkills {
		cfg.RefreshSkills = func(binaryPath string) error {
			return refreshAgentSkills(binaryPath, *configPath)
		}
	}
	updater := update.New(cfg)
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
	if result.SkillsUpdated {
		fmt.Println("Agent skill refreshed.")
	}
	if *noRestart {
		return nil
	}

	resolvedConfig, err := resolveServiceConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not resolve config for service restart: %v\n", err)
		return nil
	}
	manager, err := service.New(result.ExecutablePath, resolvedConfig)
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
  nectar update [--check] [--force] [--version vX.Y.Z] [--no-restart] [--no-skills]
                [--config PATH] [--binary PATH] [--timeout 10m] [--attempts 2] [--no-progress]

Options:
  --check          check for a newer release without installing it
  --force          reinstall even when already on the target version
  --version TAG    install a specific release (default: latest)
  --no-restart     update the binary without restarting the service
  --no-skills      update the binary without refreshing the agent skill
  --config PATH    config path used to detect the installed service
  --binary PATH    binary path to update (default: current executable)
  --timeout DUR    timeout for each download attempt (default: 10m)
  --attempts N     maximum metadata and download attempts (default: 2)
  --no-progress    disable the download progress bar
`)
}

type updateProgressPrinter struct {
	out           *os.File
	interactive   bool
	lastAsset     string
	lastMilestone int
	lastRender    time.Time
	rendered      bool
}

func newUpdateProgressPrinter(out *os.File) *updateProgressPrinter {
	interactive := false
	if info, err := out.Stat(); err == nil {
		interactive = info.Mode()&os.ModeCharDevice != 0
	}
	return &updateProgressPrinter{
		out:           out,
		interactive:   interactive,
		lastMilestone: -1,
	}
}

func (p *updateProgressPrinter) Report(progress update.Progress) {
	if progress.Asset != p.lastAsset {
		p.lastAsset = progress.Asset
		p.lastMilestone = -1
	}
	if progress.Total <= 0 {
		if progress.Done {
			if p.interactive && p.rendered {
				fmt.Fprintln(p.out)
			}
			fmt.Fprintf(p.out, "  downloaded %s\n", formatUpdateBytes(progress.Downloaded))
		}
		return
	}
	percent := int(progress.Downloaded * 100 / progress.Total)
	if percent > 100 {
		percent = 100
	}
	if p.interactive {
		if !progress.Done && time.Since(p.lastRender) < 200*time.Millisecond {
			return
		}
		barWidth := 24
		filled := percent * barWidth / 100
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		fmt.Fprintf(p.out, "\r  %3d%% [%s] %s / %s",
			percent, bar, formatUpdateBytes(progress.Downloaded), formatUpdateBytes(progress.Total))
		p.lastRender = time.Now()
		p.rendered = true
		if progress.Done {
			fmt.Fprintln(p.out)
			p.rendered = false
		}
		return
	}
	milestone := percent / 25 * 25
	if progress.Done {
		milestone = 100
	}
	if milestone == p.lastMilestone {
		return
	}
	p.lastMilestone = milestone
	fmt.Fprintf(p.out, "  %3d%% %s / %s\n",
		milestone, formatUpdateBytes(progress.Downloaded), formatUpdateBytes(progress.Total))
}

func formatUpdateBytes(value int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case value >= gib:
		return fmt.Sprintf("%.1f GiB", float64(value)/gib)
	case value >= mib:
		return fmt.Sprintf("%.1f MiB", float64(value)/mib)
	case value >= kib:
		return fmt.Sprintf("%.1f KiB", float64(value)/kib)
	default:
		return fmt.Sprintf("%d B", value)
	}
}
