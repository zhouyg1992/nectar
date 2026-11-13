package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/engine"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
)

const defaultMaxOutputBytes = 2 << 20

type Config struct {
	Executable     string
	Model          string
	Agent          string
	WorkingDir     string
	Timeout        time.Duration
	MaxOutputBytes int
	Standalone     bool
	AutoApprove    bool
}

type Adapter struct {
	cfg Config

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func NewAdapter(cfg Config) *Adapter {
	if cfg.Executable == "" {
		cfg.Executable = "opencode"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	return &Adapter{cfg: cfg, running: make(map[string]context.CancelFunc)}
}

func (a *Adapter) Name() string { return "opencode" }

func (a *Adapter) Start(ctx context.Context, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	return a.run(ctx, "", prompt, opts)
}

func (a *Adapter) Resume(ctx context.Context, sessionID string, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("opencode resume requires a session id")
	}
	return a.run(ctx, sessionID, prompt, opts)
}

func (a *Adapter) Cancel(_ context.Context, sessionID string) error {
	a.mu.Lock()
	cancel, ok := a.running[sessionID]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("no running opencode session %q", sessionID)
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
		return nil, errors.New("opencode working directory is empty")
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

	args := a.buildArgs(sessionID, prompt, opts)
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
		return nil, fmt.Errorf("open opencode stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, max: 256 * 1024}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start opencode: %w", err)
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

		parsedUsage, parseErr := ParseWithUsage(stdout, emit, maxOutput)
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
			done := engine.Event{Type: engine.EventDone}
			if parseErr == nil && trackedSession != "" {
				contextUsage := a.sessionUsage(trackedSession, workdir)
				if contextUsage == nil && parsedUsage != nil && !parsedUsage.Last.empty() {
					last := parsedUsage.Last
					contextUsage = &last
				}
				var turnUsage *tokenUsage
				if parsedUsage != nil && !parsedUsage.Turn.empty() {
					turn := parsedUsage.Turn
					turnUsage = &turn
				}
				if meta := opencodeUsageMeta(contextUsage, turnUsage); meta != nil {
					done.Meta = meta
				}
			}
			emit(done)
		}
	}()

	return events, nil
}

type tokenUsage struct {
	Input     int        `json:"input"`
	Output    int        `json:"output"`
	Reasoning int        `json:"reasoning"`
	Cache     cacheUsage `json:"cache"`
}

type cacheUsage struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

func (u tokenUsage) contextTokens() int {
	return u.Input + u.Output + u.Reasoning + u.Cache.Read + u.Cache.Write
}

func (u tokenUsage) empty() bool {
	return u.contextTokens() == 0
}

func (u tokenUsage) add(other tokenUsage) tokenUsage {
	return tokenUsage{
		Input:     u.Input + other.Input,
		Output:    u.Output + other.Output,
		Reasoning: u.Reasoning + other.Reasoning,
		Cache: cacheUsage{
			Read:  u.Cache.Read + other.Cache.Read,
			Write: u.Cache.Write + other.Cache.Write,
		},
	}
}

func opencodeUsageMeta(contextUsage, turnUsage *tokenUsage) map[string]any {
	data := map[string]int{}
	if contextUsage != nil && !contextUsage.empty() {
		data["context_tokens"] = contextUsage.contextTokens()
	}
	usage := turnUsage
	if usage == nil {
		usage = contextUsage
	}
	if usage != nil && !usage.empty() {
		data["input_tokens"] = usage.Input
		data["cached_input_tokens"] = usage.Cache.Read + usage.Cache.Write
		data["output_tokens"] = usage.Output + usage.Reasoning
	}
	if len(data) == 0 {
		return nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return map[string]any{"usage": string(encoded)}
}

func (a *Adapter) sessionUsage(sessionID, workdir string) *tokenUsage {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	args := []string{"session", "export"}
	if a.cfg.Standalone {
		args = append(args, "--standalone")
	}
	args = append(args, sessionID)
	cmd := exec.CommandContext(ctx, a.cfg.Executable, args...)
	cmd.Dir = workdir
	cmd.Env = engineEnv(workdir, nil)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var exported struct {
		Info struct {
			Tokens tokenUsage `json:"tokens"`
		} `json:"info"`
		Messages []struct {
			Type   string     `json:"type"`
			Tokens tokenUsage `json:"tokens"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &exported); err != nil {
		return nil
	}

	// The last assistant message describes the final model call, which is the
	// context usage users expect to see in the card footer.
	for i := len(exported.Messages) - 1; i >= 0; i-- {
		message := exported.Messages[i]
		if message.Type == "assistant" && !message.Tokens.empty() {
			tokens := message.Tokens
			return &tokens
		}
	}
	if exported.Info.Tokens.empty() {
		return nil
	}
	return &exported.Info.Tokens
}

func (a *Adapter) buildArgs(sessionID, prompt string, opts engine.EngineOptions) []string {
	model := firstNonEmpty(opts.Model, a.cfg.Model)
	args := []string{"run", "--format", "json"}
	if a.cfg.Standalone {
		args = append(args, "--standalone")
	}
	if a.cfg.AutoApprove {
		args = append(args, "--auto")
	}
	if sessionID != "" {
		args = append(args, "--session", sessionID)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if a.cfg.Agent != "" {
		args = append(args, "--agent", a.cfg.Agent)
	}
	for _, item := range opts.Attachments {
		if item.Kind == attachment.KindImage && strings.TrimSpace(item.Path) != "" {
			args = append(args, "--file", item.Path)
		}
	}
	args = append(args, prompt)
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

func processError(ctx context.Context, waitErr error, stderr string) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "opencode execution timed out"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "opencode execution canceled"
	}
	msg := strings.TrimSpace(stderr)
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
	env = withoutEnv(env, "PWD")
	env = withoutEnv(env, "OLDPWD")
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
