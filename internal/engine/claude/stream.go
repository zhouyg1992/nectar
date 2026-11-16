package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zhouyg1992/nectar/internal/engine"
)

// StreamAdapter uses Claude Code's streaming-input mode. It keeps the CLI
// process alive for the turn so another user message can be injected while
// Claude is still working.
type StreamAdapter struct {
	cfg Config

	mu   sync.Mutex
	runs map[string]*streamRun
}

type streamRun struct {
	adapter *StreamAdapter
	ctx     context.Context
	cancel  context.CancelFunc
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  *limitedWriter
	events  chan engine.Event
	done    chan struct{}

	writeMu  sync.Mutex
	mu       sync.Mutex
	session  string
	finished bool
}

func NewStreamAdapter(cfg Config) *StreamAdapter {
	if cfg.Executable == "" {
		cfg.Executable = "claude"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	return &StreamAdapter{cfg: cfg, runs: make(map[string]*streamRun)}
}

func (a *StreamAdapter) Name() string { return "claude" }

func (a *StreamAdapter) Start(ctx context.Context, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	return a.run(ctx, "", prompt, opts)
}

func (a *StreamAdapter) Resume(ctx context.Context, sessionID string, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("claude stream resume requires a session id")
	}
	return a.run(ctx, sessionID, prompt, opts)
}

func (a *StreamAdapter) Steer(ctx context.Context, sessionID string, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("empty steering prompt")
	}
	a.mu.Lock()
	run := a.runs[strings.TrimSpace(sessionID)]
	a.mu.Unlock()
	if run == nil {
		return engine.ErrNoActiveTurn
	}
	return run.sendUser(ctx, text)
}

func (a *StreamAdapter) Cancel(_ context.Context, sessionID string) error {
	a.mu.Lock()
	run := a.runs[strings.TrimSpace(sessionID)]
	a.mu.Unlock()
	if run == nil {
		return fmt.Errorf("no running claude session %q", sessionID)
	}
	run.cancel()
	return nil
}

func (a *StreamAdapter) track(sessionID string, run *streamRun) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	a.mu.Lock()
	a.runs[sessionID] = run
	a.mu.Unlock()
}

func (a *StreamAdapter) untrack(sessionID string) {
	a.mu.Lock()
	delete(a.runs, sessionID)
	a.mu.Unlock()
}

func (a *StreamAdapter) run(parent context.Context, sessionID, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("empty prompt")
	}
	workdir := firstNonEmpty(opts.WorkingDir, a.cfg.WorkingDir)
	if workdir == "" {
		return nil, errors.New("claude working directory is empty")
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

	cmd := exec.CommandContext(runCtx, a.cfg.Executable, a.buildStreamArgs(sessionID, opts)...)
	cmd.Dir = workdir
	cmd.Env = engineEnv(workdir, opts.Env)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 5 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open claude stream stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open claude stream stdout: %w", err)
	}
	var stderr bytes.Buffer
	stderrWriter := &limitedWriter{w: &stderr, max: 256 * 1024}
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start claude stream: %w", err)
	}

	run := &streamRun{
		adapter: a,
		ctx:     runCtx,
		cancel:  cancel,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderrWriter,
		events:  make(chan engine.Event, 32),
		done:    make(chan struct{}),
		session: sessionID,
	}
	if sessionID != "" {
		a.track(sessionID, run)
	}
	go run.readAndWait(maxOutput)

	if err := run.sendUser(runCtx, prompt); err != nil {
		cancel()
		return nil, fmt.Errorf("send initial claude prompt: %w", err)
	}
	return run.events, nil
}

func (a *StreamAdapter) buildStreamArgs(sessionID string, opts engine.EngineOptions) []string {
	model := firstNonEmpty(opts.Model, a.cfg.Model)
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
	}
	if a.cfg.AutoApprove {
		args = append(args, "--dangerously-skip-permissions")
	} else if mode := strings.TrimSpace(a.cfg.PermissionMode); mode != "" {
		args = append(args, "--permission-mode", mode)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if fallback := strings.TrimSpace(a.cfg.FallbackModel); fallback != "" {
		args = append(args, "--fallback-model", fallback)
	}
	if a.cfg.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprint(a.cfg.MaxTurns))
	}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	return args
}

func (r *streamRun) sendUser(ctx context.Context, text string) error {
	message := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": text,
		},
		"parent_tool_use_id": nil,
	}
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode claude user message: %w", err)
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.mu.Lock()
	finished := r.finished
	r.mu.Unlock()
	if finished {
		return engine.ErrNoActiveTurn
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		return io.EOF
	default:
	}
	if _, err := r.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write claude user message: %w", err)
	}
	return nil
}

func (r *streamRun) readAndWait(maxOutput int) {
	doneSent := false
	emit := func(event engine.Event) {
		if event.Type == engine.EventSessionStarted && event.SessionID != "" {
			r.mu.Lock()
			old := r.session
			r.session = event.SessionID
			r.mu.Unlock()
			if old != "" && old != event.SessionID {
				r.adapter.untrack(old)
			}
			r.adapter.track(event.SessionID, r)
		}
		if event.Type == engine.EventDone {
			r.mu.Lock()
			r.finished = true
			r.mu.Unlock()
			doneSent = true
		}
		select {
		case r.events <- event:
		case <-r.ctx.Done():
		}
		if event.Type == engine.EventDone {
			r.cancel()
		}
	}

	parseErr := Parse(r.stdout, emit, maxOutput)
	waitErr := r.cmd.Wait()
	if parseErr != nil && r.ctx.Err() == nil {
		emit(engine.Event{Type: engine.EventError, Text: parseErr.Error()})
	}
	if !doneSent {
		if waitErr != nil {
			emit(engine.Event{Type: engine.EventError, Text: processError(r.ctx, waitErr, r.stderr.w.String())})
		} else {
			emit(engine.Event{Type: engine.EventError, Text: "claude stream exited before the turn completed"})
		}
		emit(engine.Event{Type: engine.EventDone})
	}
	close(r.done)
	r.mu.Lock()
	sessionID := r.session
	r.mu.Unlock()
	if sessionID != "" {
		r.adapter.untrack(sessionID)
	}
	close(r.events)
}

var _ engine.EngineAdapter = (*StreamAdapter)(nil)
var _ engine.Steerer = (*StreamAdapter)(nil)
