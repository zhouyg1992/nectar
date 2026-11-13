//go:build linux

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func New(binaryPath, configPath string) (Service, error) {
	manager, err := BaseManager(binaryPath, configPath)
	if err != nil {
		return nil, err
	}
	manager.Label = systemdUnit
	manager.ServiceFile = filepath.Join(manager.Home, ".config", "systemd", "user", systemdUnit)
	return manager, nil
}

func (m *Manager) Installed() bool {
	_, err := os.Stat(m.ServiceFile)
	return err == nil
}

func (m *Manager) Install(ctx context.Context, opts InstallOptions) error {
	if err := requireCommand("systemctl"); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.ServiceFile), 0o755); err != nil {
		return fmt.Errorf("create systemd user directory: %w", err)
	}
	content, err := renderSystemd(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.ServiceFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}

	if err := run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := run(ctx, "systemctl", "--user", "enable", m.Label); err != nil {
		return err
	}
	if opts.Linger {
		if _, err := runQuiet(ctx, "loginctl", "enable-linger", m.User); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not enable linger: %v\n", err)
			fmt.Fprintf(os.Stderr, "run manually: sudo loginctl enable-linger %s\n", m.User)
		}
	}
	if opts.Start {
		if err := run(ctx, "systemctl", "--user", "restart", m.Label); err != nil {
			return err
		}
		fmt.Println("service started")
	} else {
		fmt.Println("service installed and enabled but not started; run: nectar service start")
	}
	fmt.Println("service file:", m.ServiceFile)
	fmt.Println("logs: journalctl --user -u", m.Label, "-f")
	return nil
}

func (m *Manager) Uninstall(ctx context.Context) error {
	_, _ = runQuiet(ctx, "systemctl", "--user", "disable", "--now", m.Label)
	if err := os.Remove(m.ServiceFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove systemd unit: %w", err)
	}
	_, _ = runQuiet(ctx, "systemctl", "--user", "daemon-reload")
	fmt.Println("service uninstalled:", m.ServiceFile)
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	if _, err := os.Stat(m.ServiceFile); err != nil {
		return fmt.Errorf("service is not installed; run: nectar service install")
	}
	return run(ctx, "systemctl", "--user", "start", m.Label)
}

func (m *Manager) Stop(ctx context.Context) error {
	return run(ctx, "systemctl", "--user", "stop", m.Label)
}

func (m *Manager) Restart(ctx context.Context) error {
	if _, err := os.Stat(m.ServiceFile); err != nil {
		return fmt.Errorf("service is not installed; run: nectar service install")
	}
	return run(ctx, "systemctl", "--user", "restart", m.Label)
}

func (m *Manager) Status(ctx context.Context) error {
	if err := requireCommand("systemctl"); err != nil {
		return err
	}
	out, err := runQuiet(ctx, "systemctl", "--user", "status", m.Label, "--no-pager")
	fmt.Print(out)
	if err != nil && strings.Contains(out, "could not be found") {
		fmt.Println("service file:", m.ServiceFile)
	}
	return nil
}

func (m *Manager) Logs(ctx context.Context, lines int, follow bool) error {
	if err := requireCommand("journalctl"); err != nil {
		return err
	}
	if lines <= 0 {
		lines = 200
	}
	args := []string{"--user", "-u", m.Label, "-n", strconv.Itoa(lines)}
	if follow {
		args = append(args, "-f")
	}
	return run(ctx, "journalctl", args...)
}

var _ Service = (*Manager)(nil)
