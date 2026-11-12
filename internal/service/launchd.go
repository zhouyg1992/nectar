//go:build darwin

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func New(binaryPath, configPath string) (Service, error) {
	manager, err := BaseManager(binaryPath, configPath)
	if err != nil {
		return nil, err
	}
	manager.Label = launchdLabel
	manager.ServiceFile = filepath.Join(manager.Home, "Library", "LaunchAgents", launchdLabel+".plist")
	return manager, nil
}

func (m *Manager) Install(ctx context.Context, opts InstallOptions) error {
	if err := requireCommand("launchctl"); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.ServiceFile), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(m.Home, "Library", "Logs"), 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	content, err := renderLaunchd(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.ServiceFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write launch agent: %w", err)
	}

	domain := launchdDomain()
	_, _ = runQuiet(ctx, "launchctl", "bootout", domain, m.ServiceFile)
	if opts.Start {
		if err := run(ctx, "launchctl", "bootstrap", domain, m.ServiceFile); err != nil {
			return err
		}
		_, _ = runQuiet(ctx, "launchctl", "enable", domain+"/"+m.Label)
		if err := run(ctx, "launchctl", "kickstart", "-k", domain+"/"+m.Label); err != nil {
			return err
		}
		fmt.Println("service started")
	} else {
		fmt.Println("service installed but not started; run: nectar service start")
	}
	fmt.Println("service file:", m.ServiceFile)
	outLog, errLog := m.logPaths()
	fmt.Println("logs:", outLog, errLog)
	return nil
}

func (m *Manager) Uninstall(ctx context.Context) error {
	_, _ = runQuiet(ctx, "launchctl", "bootout", launchdDomain(), m.ServiceFile)
	if err := os.Remove(m.ServiceFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launch agent: %w", err)
	}
	fmt.Println("service uninstalled:", m.ServiceFile)
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	if _, err := os.Stat(m.ServiceFile); err != nil {
		return fmt.Errorf("service is not installed; run: nectar service install")
	}
	domain := launchdDomain()
	_, _ = runQuiet(ctx, "launchctl", "bootstrap", domain, m.ServiceFile)
	_, _ = runQuiet(ctx, "launchctl", "enable", domain+"/"+m.Label)
	return run(ctx, "launchctl", "kickstart", "-k", domain+"/"+m.Label)
}

func (m *Manager) Stop(ctx context.Context) error {
	if err := run(ctx, "launchctl", "bootout", launchdDomain(), m.ServiceFile); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

func (m *Manager) Restart(ctx context.Context) error {
	if _, err := os.Stat(m.ServiceFile); err != nil {
		return fmt.Errorf("service is not installed; run: nectar service install")
	}
	domain := launchdDomain()
	target := domain + "/" + m.Label
	if _, err := runQuiet(ctx, "launchctl", "print", target); err != nil {
		if _, err := runQuiet(ctx, "launchctl", "bootstrap", domain, m.ServiceFile); err != nil {
			return err
		}
	}
	return run(ctx, "launchctl", "kickstart", "-k", target)
}

func (m *Manager) Status(ctx context.Context) error {
	out, err := runQuiet(ctx, "launchctl", "list", m.Label)
	if err != nil {
		fmt.Println("service is not loaded")
		fmt.Println("service file:", m.ServiceFile)
		return nil
	}
	fmt.Print(out)
	return nil
}

func (m *Manager) Logs(ctx context.Context, lines int, follow bool) error {
	if lines <= 0 {
		lines = 200
	}
	outLog, errLog := m.logPaths()
	if !fileExists(outLog) && !fileExists(errLog) {
		fmt.Println("no logs yet:", outLog, errLog)
		return nil
	}
	args := []string{"-n", strconv.Itoa(lines)}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, outLog, errLog)
	return run(ctx, "tail", args...)
}

func launchdDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

var _ Service = (*Manager)(nil)
