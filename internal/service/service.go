package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type InstallOptions struct {
	Linger bool
	Start  bool
}

type Service interface {
	Installed() bool
	Install(context.Context, InstallOptions) error
	Uninstall(context.Context) error
	Start(context.Context) error
	Stop(context.Context) error
	Restart(context.Context) error
	Status(context.Context) error
	Logs(context.Context, int, bool, []string) error
}

type Manager struct {
	Home        string
	User        string
	BinaryPath  string
	ConfigPath  string
	ServiceFile string
	Label       string
}

func BaseManager(binaryPath, configPath string) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	user := strings.TrimSpace(os.Getenv("USER"))
	if user == "" {
		user = strings.TrimSpace(os.Getenv("LOGNAME"))
	}
	if user == "" {
		return nil, errors.New("could not determine current user")
	}

	binaryPath, err = AbsolutePath(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("resolve binary path: %w", err)
	}
	configPath, err = AbsolutePath(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	return &Manager{
		Home:       home,
		User:       user,
		BinaryPath: binaryPath,
		ConfigPath: configPath,
	}, nil
}

func (m *Manager) Validate() error {
	if _, err := os.Stat(m.BinaryPath); err != nil {
		return fmt.Errorf("nectar binary not found at %s: %w", m.BinaryPath, err)
	}
	if _, err := os.Stat(m.ConfigPath); err != nil {
		return fmt.Errorf("config not found at %s: %w", m.ConfigPath, err)
	}
	return nil
}

func AbsolutePath(path string) (string, error) {
	path = ExpandPath(path)
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is empty")
	}
	return filepath.Abs(path)
}

func ExpandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func requireCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}
	return nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runFiltered(ctx context.Context, name string, args []string, filters []string) error {
	if len(filters) == 0 {
		return run(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	wait := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = writer.Close()
		wait <- err
	}()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if matchesAnyFilter(line, filters) {
			fmt.Fprintln(os.Stdout, line)
		}
	}
	scanErr := scanner.Err()
	waitErr := <-wait
	if waitErr != nil && ctx.Err() == nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), waitErr)
	}
	if scanErr != nil && ctx.Err() == nil {
		return fmt.Errorf("read %s output: %w", name, scanErr)
	}
	return nil
}

func matchesAnyFilter(line string, filters []string) bool {
	for _, filter := range filters {
		if filter = strings.TrimSpace(filter); filter != "" && strings.Contains(line, filter) {
			return true
		}
	}
	return false
}

func runQuiet(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return string(out), nil
}

func buildPath(binary string, standard ...string) string {
	seen := make(map[string]bool)
	parts := make([]string, 0, len(standard)+4)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		parts = append(parts, path)
	}

	add(filepath.Dir(binary))
	for _, path := range standard {
		add(path)
	}
	for _, path := range strings.Split(os.Getenv("PATH"), ":") {
		add(path)
	}
	return strings.Join(parts, ":")
}

func xmlEscape(value string) string {
	return html.EscapeString(value)
}

func codexHome() string {
	return strings.TrimSpace(os.Getenv("CODEX_HOME"))
}
