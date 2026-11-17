package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zhouyg1992/nectar/internal/attachment"
	"github.com/zhouyg1992/nectar/internal/engine"
)

const appServerMaxEventLineBytes = 4 << 20

type AppServerAdapter struct {
	cfg Config

	mu   sync.Mutex
	runs map[string]*appServerRun
}

type appServerRun struct {
	adapter *AppServerAdapter
	ctx     context.Context
	cancel  context.CancelFunc
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  *limitedWriter
	workdir string
	opts    engine.EngineOptions

	events     chan engine.Event
	readerDone chan struct{}
	done       chan struct{}

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcEnvelope

	threadID string
	turnID   string
	turnDone bool
	closed   bool
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type appServerTurn struct {
	ThreadID string `json:"threadId"`
	Turn     struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"turn"`
}

type appServerItemNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     struct {
		ID               string `json:"id"`
		Type             string `json:"type"`
		Text             string `json:"text"`
		Phase            string `json:"phase"`
		Command          string `json:"command"`
		AggregatedOutput string `json:"aggregatedOutput"`
		Status           string `json:"status"`
		ExitCode         *int   `json:"exitCode"`
	} `json:"item"`
}

type appServerErrorNotification struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func NewAppServerAdapter(cfg Config) *AppServerAdapter {
	if cfg.Executable == "" {
		cfg.Executable = "codex"
	}
	if cfg.Sandbox == "" {
		cfg.Sandbox = "workspace-write"
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	return &AppServerAdapter{cfg: cfg, runs: make(map[string]*appServerRun)}
}

func (a *AppServerAdapter) Name() string { return "codex" }

func (a *AppServerAdapter) Start(ctx context.Context, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	return a.run(ctx, "", prompt, opts)
}

func (a *AppServerAdapter) Resume(ctx context.Context, sessionID string, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("codex app-server resume requires a session id")
	}
	return a.run(ctx, sessionID, prompt, opts)
}

func (a *AppServerAdapter) Steer(ctx context.Context, sessionID string, text string) error {
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
	return run.steer(ctx, text)
}

func (a *AppServerAdapter) Cancel(ctx context.Context, sessionID string) error {
	a.mu.Lock()
	run := a.runs[strings.TrimSpace(sessionID)]
	a.mu.Unlock()
	if run == nil {
		return fmt.Errorf("no running codex session %q", sessionID)
	}
	return run.interrupt(ctx)
}

func (a *AppServerAdapter) track(sessionID string, run *appServerRun) {
	a.mu.Lock()
	a.runs[sessionID] = run
	a.mu.Unlock()
}

func (a *AppServerAdapter) untrack(sessionID string) {
	a.mu.Lock()
	if a.runs[sessionID] != nil {
		delete(a.runs, sessionID)
	}
	a.mu.Unlock()
}

func (a *AppServerAdapter) run(parent context.Context, sessionID, prompt string, opts engine.EngineOptions) (<-chan engine.Event, error) {
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
	workdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, fmt.Errorf("resolve codex workspace: %w", err)
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

	cmd := exec.CommandContext(runCtx, a.cfg.Executable, "app-server")
	cmd.Dir = workdir
	env := withoutEnv(os.Environ(), "FEISHU_APP_SECRET")
	env = withoutEnv(env, "TELEGRAM_BOT_TOKEN")
	env = withoutEnv(env, "TELEGRAM_PROXY")
	cmd.Env = engine.EnvWith(env, opts.Env)
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
		return nil, fmt.Errorf("open codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open codex app-server stdout: %w", err)
	}
	var stderr bytes.Buffer
	stderrWriter := &limitedWriter{w: &stderr, max: 256 * 1024}
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	run := &appServerRun{
		adapter:    a,
		ctx:        runCtx,
		cancel:     cancel,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderrWriter,
		workdir:    workdir,
		opts:       opts,
		events:     make(chan engine.Event, 32),
		readerDone: make(chan struct{}),
		done:       make(chan struct{}),
		pending:    make(map[int64]chan rpcEnvelope),
	}

	go run.readLoop(maxOutput)
	go run.waitLoop()

	if err := run.initialize(); err != nil {
		run.cancel()
		return nil, err
	}
	threadID, err := run.ensureThread(sessionID)
	if err != nil {
		run.cancel()
		return nil, err
	}
	run.mu.Lock()
	run.threadID = threadID
	run.mu.Unlock()
	run.emit(engine.Event{Type: engine.EventSessionStarted, SessionID: threadID})
	a.track(threadID, run)

	if err := run.startTurn(prompt); err != nil {
		a.untrack(threadID)
		run.cancel()
		return nil, err
	}
	return run.events, nil
}

func (r *appServerRun) initialize() error {
	params := map[string]any{
		"clientInfo": map[string]any{
			"name":    "nectar",
			"version": "dev",
		},
	}
	var result json.RawMessage
	if err := r.request(r.ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize codex app-server: %w", err)
	}
	return r.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]any{},
	})
}

func (r *appServerRun) ensureThread(sessionID string) (string, error) {
	model := firstNonEmpty(r.opts.Model, r.adapter.cfg.Model)
	sandbox := firstNonEmpty(r.opts.Sandbox, r.adapter.cfg.Sandbox)
	if r.adapter.cfg.BypassApprovals {
		sandbox = "danger-full-access"
	}
	params := map[string]any{
		"cwd":            r.workdir,
		"approvalPolicy": "never",
	}
	if model != "" {
		params["model"] = model
	}
	if sandbox != "" {
		params["sandbox"] = sandbox
	}
	method := "thread/start"
	if strings.TrimSpace(sessionID) != "" {
		method = "thread/resume"
		params["threadId"] = sessionID
	}
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := r.request(r.ctx, method, params, &response); err != nil {
		return "", fmt.Errorf("%s: %w", method, err)
	}
	if strings.TrimSpace(response.Thread.ID) == "" {
		return "", fmt.Errorf("%s returned an empty thread id", method)
	}
	return response.Thread.ID, nil
}

func (r *appServerRun) startTurn(prompt string) error {
	model := firstNonEmpty(r.opts.Model, r.adapter.cfg.Model)
	effort := firstNonEmpty(r.opts.Effort, r.adapter.cfg.Effort)
	inputs := make([]map[string]any, 0, 1+len(r.opts.Attachments))
	inputs = append(inputs, map[string]any{
		"type":          "text",
		"text":          prompt,
		"text_elements": []any{},
	})
	for _, item := range r.opts.Attachments {
		if item.Kind == attachment.KindImage && strings.TrimSpace(item.Path) != "" {
			inputs = append(inputs, map[string]any{
				"type": "localImage",
				"path": item.Path,
			})
		}
	}
	params := map[string]any{
		"threadId":       r.threadID,
		"input":          inputs,
		"approvalPolicy": "never",
	}
	if model != "" {
		params["model"] = model
	}
	if effort != "" {
		params["effort"] = effort
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := r.request(r.ctx, "turn/start", params, &response); err != nil {
		return fmt.Errorf("start codex turn: %w", err)
	}
	if strings.TrimSpace(response.Turn.ID) == "" {
		return errors.New("start codex turn returned an empty turn id")
	}
	r.mu.Lock()
	r.turnID = response.Turn.ID
	r.mu.Unlock()
	return nil
}

func (r *appServerRun) steer(ctx context.Context, text string) error {
	r.mu.Lock()
	threadID := r.threadID
	turnID := r.turnID
	turnDone := r.turnDone || r.closed
	r.mu.Unlock()
	if threadID == "" || turnID == "" || turnDone {
		return engine.ErrNoActiveTurn
	}
	params := map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input": []map[string]any{{
			"type":          "text",
			"text":          text,
			"text_elements": []any{},
		}},
	}
	var response struct {
		TurnID string `json:"turnId"`
	}
	if err := r.request(ctx, "turn/steer", params, &response); err != nil {
		return err
	}
	if response.TurnID != "" && response.TurnID != turnID {
		return fmt.Errorf("codex steered turn %q, want %q", response.TurnID, turnID)
	}
	return nil
}

func (r *appServerRun) interrupt(ctx context.Context) error {
	r.mu.Lock()
	threadID := r.threadID
	turnID := r.turnID
	r.mu.Unlock()
	if threadID != "" && turnID != "" {
		interruptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = r.request(interruptCtx, "turn/interrupt", map[string]any{
			"threadId": threadID,
			"turnId":   turnID,
		}, nil)
		cancel()
	}
	r.cancel()
	return nil
}

func (r *appServerRun) request(ctx context.Context, method string, params any, out any) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return io.EOF
	}
	r.nextID++
	id := r.nextID
	ch := make(chan rpcEnvelope, 1)
	r.pending[id] = ch
	r.mu.Unlock()

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := r.writeJSON(payload); err != nil {
		r.removePending(id)
		return err
	}

	select {
	case response := <-ch:
		if response.Error != nil {
			if response.Error.Message != "" {
				return errors.New(response.Error.Message)
			}
			return fmt.Errorf("codex app-server %s failed", method)
		}
		if out != nil && len(response.Result) > 0 {
			if err := json.Unmarshal(response.Result, out); err != nil {
				return fmt.Errorf("decode %s response: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		r.removePending(id)
		return ctx.Err()
	case <-r.done:
		return io.EOF
	}
}

func (r *appServerRun) removePending(id int64) {
	r.mu.Lock()
	delete(r.pending, id)
	r.mu.Unlock()
}

func (r *appServerRun) writeJSON(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode codex app-server request: %w", err)
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if _, err := r.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write codex app-server request: %w", err)
	}
	return nil
}

func (r *appServerRun) readLoop(maxOutput int) {
	defer close(r.readerDone)
	scanner := bufio.NewScanner(r.stdout)
	maxEventLine := maxOutput
	if maxEventLine <= 0 {
		maxEventLine = appServerMaxEventLineBytes
	}
	initialBuffer := 64 * 1024
	if initialBuffer > maxEventLine {
		initialBuffer = maxEventLine
	}
	scanner.Buffer(make([]byte, initialBuffer), maxEventLine)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope rpcEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			r.emit(engine.Event{Type: engine.EventError, Text: fmt.Sprintf("parse codex app-server event: %v", err)})
			r.cancel()
			return
		}
		switch {
		case len(envelope.ID) > 0 && envelope.Method != "":
			r.handleServerRequest(envelope)
		case len(envelope.ID) > 0:
			r.handleResponse(envelope)
		case envelope.Method != "":
			r.handleNotification(envelope)
		}
	}
	if err := scanner.Err(); err != nil {
		message := fmt.Sprintf("read codex app-server output: %v", err)
		if errors.Is(err, bufio.ErrTooLong) {
			message = fmt.Sprintf("codex app-server event exceeded %d bytes", maxEventLine)
		}
		r.emit(engine.Event{Type: engine.EventError, Text: message})
		r.cancel()
	}
}

func (r *appServerRun) waitLoop() {
	<-r.readerDone
	waitErr := r.cmd.Wait()

	r.mu.Lock()
	r.closed = true
	turnDone := r.turnDone
	threadID := r.threadID
	r.mu.Unlock()
	close(r.done)

	if !turnDone {
		if waitErr != nil {
			r.emitFinal(engine.Event{Type: engine.EventError, Text: processError(r.ctx, waitErr, r.stderr.w.String())})
		} else {
			r.emitFinal(engine.Event{Type: engine.EventError, Text: "codex app-server exited before the turn completed"})
		}
		r.emitFinal(engine.Event{Type: engine.EventDone})
	}

	if threadID != "" {
		usageEvent := engine.Event{Type: engine.EventUsage}
		applyCodexSessionUsage(&usageEvent, threadID)
		if usageEvent.Meta != nil {
			r.emitFinal(usageEvent)
		}
		r.adapter.untrack(threadID)
	}
	close(r.events)
}

func (r *appServerRun) emit(event engine.Event) {
	select {
	case r.events <- event:
	case <-r.ctx.Done():
	}
}

func (r *appServerRun) emitFinal(event engine.Event) {
	r.events <- event
}

func (r *appServerRun) handleResponse(envelope rpcEnvelope) {
	id, ok := rpcID(envelope.ID)
	if !ok {
		return
	}
	r.mu.Lock()
	ch := r.pending[id]
	delete(r.pending, id)
	r.mu.Unlock()
	if ch != nil {
		ch <- envelope
	}
}

func (r *appServerRun) handleServerRequest(envelope rpcEnvelope) {
	result := any(map[string]any{})
	switch envelope.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval",
		"item/permissions/requestApproval", "applyPatchApproval", "execCommandApproval":
		result = map[string]any{"decision": "decline"}
	case "item/tool/requestUserInput":
		result = map[string]any{"answers": map[string]any{}}
	case "mcpServer/elicitation/request":
		result = map[string]any{"action": "decline"}
	}
	_ = r.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(envelope.ID),
		"result":  result,
	})
}

func (r *appServerRun) handleNotification(envelope rpcEnvelope) {
	switch envelope.Method {
	case "turn/started":
		var notification appServerTurn
		if json.Unmarshal(envelope.Params, &notification) == nil && notification.Turn.ID != "" {
			r.mu.Lock()
			r.turnID = notification.Turn.ID
			r.mu.Unlock()
		}
	case "turn/completed":
		var notification appServerTurn
		if err := json.Unmarshal(envelope.Params, &notification); err != nil {
			r.emit(engine.Event{Type: engine.EventError, Text: fmt.Sprintf("parse codex turn completion: %v", err)})
			return
		}
		if notification.Turn.Error != nil && strings.TrimSpace(notification.Turn.Error.Message) != "" {
			r.emit(engine.Event{Type: engine.EventError, Text: notification.Turn.Error.Message})
		} else if notification.Turn.Status == "failed" {
			r.emit(engine.Event{Type: engine.EventError, Text: "codex turn failed"})
		} else if notification.Turn.Status == "interrupted" {
			r.emit(engine.Event{Type: engine.EventError, Text: "codex turn interrupted"})
		}
		r.mu.Lock()
		r.turnID = notification.Turn.ID
		r.turnDone = true
		r.mu.Unlock()
		r.emit(engine.Event{Type: engine.EventDone})
		r.cancel()
	case "error":
		var notification appServerErrorNotification
		if json.Unmarshal(envelope.Params, &notification) == nil && strings.TrimSpace(notification.Error.Message) != "" {
			r.emit(engine.Event{Type: engine.EventError, Text: notification.Error.Message})
		}
	case "item/started":
		r.handleItemNotification(envelope.Params, true)
	case "item/completed":
		r.handleItemNotification(envelope.Params, false)
	}
}

func (r *appServerRun) handleItemNotification(raw json.RawMessage, started bool) {
	var notification appServerItemNotification
	if err := json.Unmarshal(raw, &notification); err != nil {
		return
	}
	item := notification.Item
	switch item.Type {
	case "commandExecution":
		if started {
			r.emit(engine.Event{
				Type: engine.EventToolCall,
				Text: item.Command,
				Meta: map[string]any{"item_id": item.ID, "tool": "command_execution"},
			})
			return
		}
		meta := map[string]any{
			"item_id": item.ID,
			"tool":    "command_execution",
			"status":  item.Status,
			"command": item.Command,
		}
		if item.ExitCode != nil {
			meta["exit_code"] = *item.ExitCode
		}
		r.emit(engine.Event{Type: engine.EventToolResult, Text: item.AggregatedOutput, Meta: meta})
	case "mcpToolCall":
		if started {
			r.emit(engine.Event{Type: engine.EventToolCall, Text: item.Command, Meta: map[string]any{"item_id": item.ID, "tool": "mcp_tool_call"}})
			return
		}
		r.emit(engine.Event{Type: engine.EventToolResult, Text: item.AggregatedOutput, Meta: map[string]any{"item_id": item.ID, "tool": "mcp_tool_call", "status": item.Status}})
	case "agentMessage":
		if started {
			return
		}
		meta := map[string]any{}
		if item.Phase != "" {
			meta["phase"] = item.Phase
		}
		r.emit(engine.Event{Type: engine.EventMessage, Text: item.Text, Meta: meta})
	case "reasoning":
		if !started {
			r.emit(engine.Event{Type: engine.EventReasoning})
		}
	}
}

func rpcID(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		number, err = strconv.ParseInt(text, 10, 64)
		return number, err == nil
	}
	return 0, false
}

var _ engine.EngineAdapter = (*AppServerAdapter)(nil)
var _ engine.Steerer = (*AppServerAdapter)(nil)
