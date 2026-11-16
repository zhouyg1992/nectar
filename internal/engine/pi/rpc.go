package pi

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

// RPCAdapter keeps Pi in RPC mode for the lifetime of one turn. That is the
// transport that exposes Pi's native steering queue.
type RPCAdapter struct {
	cfg Config

	mu   sync.Mutex
	runs map[string]*rpcRun
}

type rpcRun struct {
	adapter *RPCAdapter
	ctx     context.Context
	cancel  context.CancelFunc
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  *limitedWriter

	events chan engine.Event
	done   chan struct{}

	writeMu  sync.Mutex
	mu       sync.Mutex
	session  string
	finished bool
}

func NewRPCAdapter(cfg Config) *RPCAdapter {
	if cfg.Executable == "" {
		cfg.Executable = "pi"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	return &RPCAdapter{cfg: cfg, runs: make(map[string]*rpcRun)}
}

func (a *RPCAdapter) Name() string { return "pi" }

func (a *RPCAdapter) Start(ctx context.Context, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	return a.run(ctx, "", prompt, opts)
}

func (a *RPCAdapter) Resume(ctx context.Context, sessionID string, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("pi rpc resume requires a session id")
	}
	return a.run(ctx, sessionID, prompt, opts)
}

func (a *RPCAdapter) Steer(ctx context.Context, sessionID string, text string) error {
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
	return run.send(ctx, map[string]any{"type": "steer", "message": text})
}

func (a *RPCAdapter) Cancel(_ context.Context, sessionID string) error {
	a.mu.Lock()
	run := a.runs[strings.TrimSpace(sessionID)]
	a.mu.Unlock()
	if run == nil {
		return fmt.Errorf("no running pi session %q", sessionID)
	}
	run.cancel()
	return nil
}

func (a *RPCAdapter) track(sessionID string, run *rpcRun) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	a.mu.Lock()
	a.runs[sessionID] = run
	a.mu.Unlock()
}

func (a *RPCAdapter) untrack(sessionID string) {
	a.mu.Lock()
	delete(a.runs, sessionID)
	a.mu.Unlock()
}

func (a *RPCAdapter) run(parent context.Context, sessionID, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("empty prompt")
	}
	workdir := firstNonEmpty(opts.WorkingDir, a.cfg.WorkingDir)
	if workdir == "" {
		return nil, errors.New("pi working directory is empty")
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

	cmd := exec.CommandContext(runCtx, a.cfg.Executable, a.buildRPCArgs(sessionID, opts)...)
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
		return nil, fmt.Errorf("open pi rpc stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open pi rpc stdout: %w", err)
	}
	var stderr bytes.Buffer
	stderrWriter := &limitedWriter{w: &stderr, max: 256 * 1024}
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start pi rpc: %w", err)
	}

	run := &rpcRun{
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

	if err := run.send(runCtx, map[string]any{"type": "get_state"}); err != nil {
		cancel()
		return nil, fmt.Errorf("read pi rpc state: %w", err)
	}
	if err := run.send(runCtx, map[string]any{"type": "prompt", "message": prompt}); err != nil {
		cancel()
		return nil, fmt.Errorf("start pi rpc prompt: %w", err)
	}
	return run.events, nil
}

func (a *RPCAdapter) buildRPCArgs(sessionID string, opts engine.EngineOptions) []string {
	args := []string{"--mode", "rpc"}
	if a.cfg.TrustWorkspace {
		args = append(args, "--approve")
	}
	if provider := strings.TrimSpace(a.cfg.Provider); provider != "" {
		args = append(args, "--provider", provider)
	}
	if model := firstNonEmpty(opts.Model, a.cfg.Model); model != "" {
		args = append(args, "--model", model)
	}
	if thinking := strings.TrimSpace(a.cfg.Thinking); thinking != "" {
		args = append(args, "--thinking", thinking)
	}
	if sessionID != "" {
		args = append(args, "--session", sessionID)
	}
	return args
}

func (r *rpcRun) send(ctx context.Context, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode pi rpc command: %w", err)
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
		return fmt.Errorf("write pi rpc command: %w", err)
	}
	return nil
}

func (r *rpcRun) readAndWait(maxOutput int) {
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
			go func() {
				time.Sleep(20 * time.Millisecond)
				r.cancel()
			}()
		}
		select {
		case r.events <- event:
		case <-r.ctx.Done():
		}
	}

	parseErr := ParseRPC(r.stdout, emit, maxOutput)
	waitErr := r.cmd.Wait()
	if parseErr != nil && r.ctx.Err() == nil {
		emit(engine.Event{Type: engine.EventError, Text: parseErr.Error()})
	}
	if !doneSent {
		if waitErr != nil {
			emit(engine.Event{Type: engine.EventError, Text: processError(r.ctx, waitErr, r.stderr.w.String())})
		} else {
			emit(engine.Event{Type: engine.EventError, Text: "pi rpc exited before the turn completed"})
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

var _ engine.EngineAdapter = (*RPCAdapter)(nil)
var _ engine.Steerer = (*RPCAdapter)(nil)
