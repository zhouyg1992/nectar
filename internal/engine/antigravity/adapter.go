package antigravity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
)

const defaultMaxOutputBytes = 2 << 20

type Config struct {
	Executable     string
	Model          string
	Effort         string
	Agent          string
	Mode           string
	Sandbox        bool
	AutoApprove    bool
	WorkingDir     string
	Timeout        time.Duration
	MaxOutputBytes int
}

type Adapter struct {
	cfg Config

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func NewAdapter(cfg Config) *Adapter {
	if cfg.Executable == "" {
		cfg.Executable = "agy"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	return &Adapter{cfg: cfg, running: make(map[string]context.CancelFunc)}
}

func (a *Adapter) Name() string { return "antigravity" }

func (a *Adapter) Start(ctx context.Context, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	return a.run(ctx, "", prompt, opts)
}

func (a *Adapter) Resume(ctx context.Context, sessionID string, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("antigravity resume requires a conversation id")
	}
	return a.run(ctx, sessionID, prompt, opts)
}

func (a *Adapter) Cancel(_ context.Context, sessionID string) error {
	a.mu.Lock()
	cancel, ok := a.running[sessionID]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running antigravity session %q", sessionID)
	}
	cancel()
	return nil
}

func (a *Adapter) run(parent context.Context, sessionID, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("empty prompt")
	}

	workdir := firstNonEmpty(opts.WorkingDir, a.cfg.WorkingDir)
	if workdir == "" {
		return nil, errors.New("antigravity working directory is empty")
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = a.cfg.Timeout
	}
	runCtx := parent
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(parent, timeout)
	} else {
		runCtx, cancel = context.WithCancel(parent)
	}

	maxOutput := opts.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = a.cfg.MaxOutputBytes
	}

	args := a.buildArgs(sessionID, prompt, opts, timeout)
	cmd := exec.CommandContext(runCtx, a.cfg.Executable, args...)
	cmd.Dir = workdir
	cmd.Env = engineEnv(workdir, opts.Env)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open agy stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, max: 256 * 1024}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start agy: %w", err)
	}

	events := make(chan engine.Event, 16)
	go func() {
		defer close(events)
		defer cancel()

		sentDone := false
		trackedSession := sessionID
		if trackedSession != "" {
			a.track(trackedSession, cancel)
		}
		defer func() {
			if trackedSession != "" {
				a.untrack(trackedSession)
			}
		}()

		emit := func(ev engine.Event) {
			if ev.Type == engine.EventSessionStarted && ev.SessionID != "" && ev.SessionID != trackedSession {
				if trackedSession != "" {
					a.untrack(trackedSession)
				}
				trackedSession = ev.SessionID
				a.track(trackedSession, cancel)
			}
			if ev.Type == engine.EventDone {
				sentDone = true
			}
			events <- ev
		}

		parseErr := Parse(stdout, emit, maxOutput)
		if parseErr != nil && cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		waitErr := cmd.Wait()

		if parseErr != nil {
			emit(engine.Event{Type: engine.EventError, Text: parseErr.Error()})
		}
		if waitErr != nil {
			emit(engine.Event{Type: engine.EventError, Text: processError(runCtx, waitErr, stderr.String())})
		} else if !sentDone {
			emit(engine.Event{Type: engine.EventDone})
		}
	}()

	return events, nil
}

func (a *Adapter) buildArgs(sessionID, prompt string, opts engine.EngineOptions, timeout time.Duration) []string {
	model := firstNonEmpty(opts.Model, a.cfg.Model)
	effort := firstNonEmpty(opts.Effort, a.cfg.Effort)

	args := []string{"-p", prompt, "--output-format", "stream-json"}
	if timeout > 0 {
		args = append(args, "--print-timeout", fmt.Sprintf("%ds", int(timeout.Seconds())))
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	if agent := strings.TrimSpace(a.cfg.Agent); agent != "" {
		args = append(args, "--agent", agent)
	}
	if mode := strings.TrimSpace(a.cfg.Mode); mode != "" {
		args = append(args, "--mode", mode)
	}
	if a.cfg.Sandbox {
		args = append(args, "--sandbox")
	}
	if a.cfg.AutoApprove {
		args = append(args, "--dangerously-skip-permissions")
	}
	if sessionID != "" {
		args = append(args, "--conversation", sessionID)
	}
	return args
}

func (a *Adapter) track(sessionID string, cancel context.CancelFunc) {
	a.mu.Lock()
	a.running[sessionID] = cancel
	a.mu.Unlock()
}

func (a *Adapter) untrack(sessionID string) {
	a.mu.Lock()
	delete(a.running, sessionID)
	a.mu.Unlock()
}

const loginHint = "Antigravity CLI is not logged in. Run `agy` interactively as the same user running Nectar, complete the Google sign-in, then restart the Nectar service."

func antigravityAuthHint(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"not logged into antigravity",
		"authentication required",
		"login required",
		"not logged in",
	} {
		if strings.Contains(lower, marker) {
			return loginHint, true
		}
	}
	return "", false
}

func authErrorMessage(text string) string {
	if hint, ok := antigravityAuthHint(text); ok {
		return hint
	}
	return text
}

func processError(ctx context.Context, waitErr error, stderr string) string {
	msg := strings.TrimSpace(stderr)
	if hint, ok := antigravityAuthHint(msg); ok {
		return hint
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "antigravity execution timed out"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "antigravity execution canceled"
	}
	if msg != "" {
		return fmt.Sprintf("%v: %s", waitErr, execguard.Truncate(msg, 2000))
	}
	return waitErr.Error()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func withoutEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func engineEnv(workdir string, extra map[string]string) []string {
	env := withoutEnv(os.Environ(), "FEISHU_APP_SECRET")
	env = withoutEnv(env, "TELEGRAM_BOT_TOKEN")
	env = withoutEnv(env, "TELEGRAM_PROXY")
	env = withoutEnv(env, "PWD")
	env = withoutEnv(env, "OLDPWD")
	env = withoutEnv(env, "ANTIGRAVITY_LS_ADDRESS")
	env = withoutEnv(env, "ANTIGRAVITY_CSRF_TOKEN")
	env = withoutEnv(env, "ANTIGRAVITY_CONVERSATION_ID")
	env = withoutEnv(env, "ANTIGRAVITY_PROJECT_ID")
	return engine.EnvWith(append(env, "PWD="+workdir, "OLDPWD="+workdir), extra)
}

type limitedWriter struct {
	w   *bytes.Buffer
	max int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	remaining := l.max - l.w.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		if _, err := l.w.Write(p[:remaining]); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return l.w.Write(p)
}

var _ engine.EngineAdapter = (*Adapter)(nil)
