package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/engine"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
)

const defaultMaxOutputBytes = 2 << 20

type Config struct {
	Executable      string
	Model           string
	Effort          string
	Sandbox         string
	WorkingDir      string
	Timeout         time.Duration
	MaxOutputBytes  int
	BypassApprovals bool
}

type Adapter struct {
	cfg Config

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func NewAdapter(cfg Config) *Adapter {
	if cfg.Executable == "" {
		cfg.Executable = "codex"
	}
	if cfg.Sandbox == "" {
		cfg.Sandbox = "workspace-write"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	return &Adapter{cfg: cfg, running: make(map[string]context.CancelFunc)}
}

func (a *Adapter) Name() string { return "codex" }

func (a *Adapter) Start(ctx context.Context, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	return a.run(ctx, "", prompt, opts)
}

func (a *Adapter) Resume(ctx context.Context, sessionID string, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("codex resume requires a session id")
	}
	return a.run(ctx, sessionID, prompt, opts)
}

func (a *Adapter) Cancel(_ context.Context, sessionID string) error {
	a.mu.Lock()
	cancel, ok := a.running[sessionID]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running codex session %q", sessionID)
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
		return nil, errors.New("codex working directory is empty")
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

	args := a.buildArgs(sessionID, opts)
	cmd := exec.CommandContext(runCtx, a.cfg.Executable, args...)
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(prompt)
	// The agent should not inherit the bot's Feishu credential.
	cmd.Env = withoutEnv(os.Environ(), "FEISHU_APP_SECRET")
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
		return nil, fmt.Errorf("open codex stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, max: 256 * 1024}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start codex: %w", err)
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

func (a *Adapter) buildArgs(sessionID string, opts engine.EngineOptions) []string {
	model := firstNonEmpty(opts.Model, a.cfg.Model)
	effort := firstNonEmpty(opts.Effort, a.cfg.Effort)
	sandbox := firstNonEmpty(opts.Sandbox, a.cfg.Sandbox)
	bypass := a.cfg.BypassApprovals

	args := []string{"exec"}
	if sessionID != "" {
		args = append(args, "resume", sessionID)
	}
	args = append(args, "--json", "--skip-git-repo-check")
	// Keep exec non-interactive even if the user's config asks for approvals.
	args = append(args, "--config", `approval_policy="never"`)

	if bypass {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else if sessionID == "" && sandbox != "" {
		args = append(args, "--sandbox", sandbox)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--config", "model_reasoning_effort="+strconv.Quote(effort))
	}
	for _, item := range opts.Attachments {
		if item.Kind == attachment.KindImage && strings.TrimSpace(item.Path) != "" {
			args = append(args, "--image", item.Path)
		}
	}

	// "-" makes Codex read the prompt from stdin, avoiding argv limits.
	args = append(args, "-")
	return args
}

func processError(ctx context.Context, waitErr error, stderr string) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "codex execution timed out"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "codex execution canceled"
	}
	msg := strings.TrimSpace(stderr)
	if msg != "" {
		return fmt.Sprintf("%v: %s", waitErr, execguard.Truncate(msg, 2000))
	}
	return waitErr.Error()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
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
