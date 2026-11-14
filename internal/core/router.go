package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zhouyg1992/nectar/internal/core/decision"
	"github.com/zhouyg1992/nectar/internal/engine"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/memory"
	"github.com/zhouyg1992/nectar/internal/memory/comb"
	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	maxReplyBytes     = 60_000
	cardTitleThinking = "🌼 Thinking..."
	cardTitleWorking  = "🐝 Working..."
	cardTitleComplete = "🍯 Complete"
	cardTitleFailed   = "⚠️ Failed"
)

type RouterConfig struct {
	AgentID                string
	BotName                string
	Version                string
	ModelName              string
	ContextWindow          int
	CardUpdateInterval     time.Duration
	EngineOptions          engine.EngineOptions
	AdminUsers             []string
	DataDir                string
	APISocket              string
	MemoryMaxItems         int
	MemoryMaxChars         int
	DisableMemoryAutoLearn bool
}

type Router struct {
	ctx        context.Context
	platform   platform.PlatformAdapter
	engine     engine.EngineAdapter
	sessions   *SessionStore
	queue      *Queue
	workspaces *execguard.Workspace
	lessons    comb.Store
	memories   memory.Store
	memorySeen sync.Map
	decider    decision.DecisionService
	anchors    *AnchorStore
	scheduler  *Scheduler
	logger     *slog.Logger
	cfg        RouterConfig
}

func NewRouter(
	ctx context.Context,
	plat platform.PlatformAdapter,
	eng engine.EngineAdapter,
	sessions *SessionStore,
	queue *Queue,
	workspaces *execguard.Workspace,
	lessons comb.Store,
	memories memory.Store,
	decider decision.DecisionService,
	logger *slog.Logger,
	cfg RouterConfig,
) *Router {
	if cfg.AgentID == "" {
		cfg.AgentID = "codex"
	}
	if cfg.BotName == "" {
		cfg.BotName = "Nectar"
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.CardUpdateInterval <= 0 {
		cfg.CardUpdateInterval = 2 * time.Second
	}
	if cfg.MemoryMaxItems <= 0 {
		cfg.MemoryMaxItems = 6
	}
	if cfg.MemoryMaxChars <= 0 {
		cfg.MemoryMaxChars = 1500
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{
		ctx:        ctx,
		platform:   plat,
		engine:     eng,
		sessions:   sessions,
		queue:      queue,
		workspaces: workspaces,
		lessons:    lessons,
		memories:   memories,
		decider:    decider,
		logger:     logger,
		cfg:        cfg,
	}
}

func (r *Router) SetScheduling(anchors *AnchorStore, scheduler *Scheduler) {
	r.anchors = anchors
	r.scheduler = scheduler
}

func (r *Router) Handle(msg platform.Message) {
	key := SessionKey{
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		ThreadID: msg.ThreadID,
		AgentID:  r.cfg.AgentID,
	}
	if r.anchors != nil && msg.Origin != "schedule" {
		if err := r.anchors.Set(key, SessionAnchor{
			MessageID: msg.MessageID,
			Channel:   msg.Channel,
			ChatID:    msg.ChatID,
			ThreadID:  msg.ThreadID,
			ChatType:  msg.ChatType,
			UserID:    msg.UserID,
		}); err != nil {
			r.logger.Warn("could not persist delivery anchor", "error", err)
		}
	}
	text := strings.TrimSpace(msg.Text)

	if msg.Origin != "schedule" {
		switch text {
		case "/new", "/reset":
			if err := r.sessions.Delete(key); err != nil {
				r.logger.Warn("could not persist session reset", "error", err)
			}
			r.sendCommandResult(msg, platform.CardInfo, "🌱 New Session", "Started a new session.")
			return
		case "/status":
			r.sendCommandResult(msg, platform.CardInfo, "🌿 Session Status", r.status(key))
			return
		case "/about", "/whoami":
			r.sendCommandResult(msg, platform.CardInfo, "🍯 About "+r.cfg.BotName, r.aboutText())
			return
		case "/stop":
			r.stop(key, msg)
			return
		}

		if isScheduleCommand(text) {
			r.handleScheduleCommand(key, msg, text)
			return
		}

		if isMemoryCommand(text) {
			r.handleMemoryCommand(msg, text)
			return
		}

		if isHelpRequest(text) {
			r.sendHelp(msg)
			return
		}
	}

	if text == "" && len(msg.Attachments) == 0 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Unsupported Message", "Only text, image, or file messages are supported.")
		return
	}

	// Platform callbacks must return fast (Feishu retries after ~3s), so the
	// actual agent run is queued and serialized per session key.
	r.queue.Do(key.String(), func() {
		r.execute(r.ctx, key, msg, text)
	})
}

// ExecuteScheduled runs a scheduler-owned prompt through the same per-session
// queue as chat messages and waits until that turn finishes. The scheduler uses
// the returned error to report the actual agent result instead of merely
// whether the prompt was enqueued.
func (r *Router) ExecuteScheduled(ctx context.Context, msg platform.Message) error {
	msg.Origin = "schedule"
	text := strings.TrimSpace(msg.Text)
	if text == "" && len(msg.Attachments) == 0 {
		return errors.New("scheduled prompt is empty")
	}
	key := SessionKey{
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		ThreadID: msg.ThreadID,
		AgentID:  r.cfg.AgentID,
	}
	done := make(chan error, 1)
	r.queue.Do(key.String(), func() {
		if err := ctx.Err(); err != nil {
			done <- err
			return
		}
		done <- r.execute(ctx, key, msg, text)
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Router) execute(ctx context.Context, key SessionKey, msg platform.Message, prompt string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	workdir, err := r.workspaces.ForChat(msg.ChatID)
	if err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Workspace Error", "❌ Failed to create workspace: "+err.Error())
		return err
	}

	opts := r.cfg.EngineOptions
	opts.WorkingDir = workdir
	opts.Env = map[string]string{
		"NECTAR_CHANNEL":      key.Channel,
		"NECTAR_CHAT_ID":      key.ChatID,
		"NECTAR_CHAT_TYPE":    msg.ChatType,
		"NECTAR_USER_ID":      msg.UserID,
		"NECTAR_AGENT_ID":     key.AgentID,
		"NECTAR_PRODUCT_NAME": r.cfg.BotName,
		"NECTAR_VERSION":      r.cfg.Version,
		"NECTAR_MODEL":        r.resolvedModel(),
		"NECTAR_PLATFORM":     r.platform.Name(),
		"NECTAR_DATA_DIR":     r.cfg.DataDir,
		"NECTAR_API_SOCKET":   r.cfg.APISocket,
		"NECTAR_WORKING_DIR":  workdir,
	}
	if executable, execErr := os.Executable(); execErr == nil {
		opts.Env["NECTAR_CLI"] = executable
	}
	if key.ThreadID != "" {
		opts.Env["NECTAR_THREAD_ID"] = key.ThreadID
	}
	startedAt := time.Now()
	modelName := r.resolvedModel()

	cardID := ""
	caps := r.platform.Capabilities()
	stopTyping := func() {}
	if typing, ok := r.platform.(platform.TypingIndicator); ok {
		if stop := typing.StartTyping(r.ctx, msg.ReplyContext); stop != nil {
			stopTyping = stop
		}
	}
	defer stopTyping()

	if caps.Card && caps.Streaming {
		initial := &platform.Card{
			Status: platform.CardThinking,
			Title:  cardTitleThinking,
			Body:   initialCardBody(len(msg.Attachments) > 0),
		}
		reply := replyTarget(msg)
		reply.Card = initial
		id, sendErr := r.platform.Send(r.ctx, reply)
		if sendErr != nil {
			r.logger.Error("send thinking card failed", "chat", msg.ChatID, "error", sendErr)
		} else {
			cardID = id
		}
	}

	attachments, cleanupAttachments, err := r.prepareAttachments(msg, workdir)
	if err != nil {
		answer := "❌ Failed to download attachments: " + err.Error()
		finishErr := r.finish(msg, cardID, &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, time.Since(startedAt), nil, r.cfg.ContextWindow),
		}, answer)
		return errors.Join(err, finishErr)
	}
	defer cleanupAttachments()
	lessonPrompt := prompt
	if strings.TrimSpace(lessonPrompt) == "" && len(attachments) > 0 {
		lessonPrompt = defaultAttachmentPrompt(attachments)
	}
	prompt = buildAttachmentPrompt(prompt, attachments)
	prompt = r.withIdentity(prompt)
	opts.Attachments = attachments

	sessionID, hasSession := r.sessions.Get(key)
	prompt = r.withMemory(ctx, msg, workdir, prompt, key, hasSession)
	var events <-chan engine.Event
	sessionNotice := ""
	if hasSession {
		events, err = r.engine.Resume(ctx, sessionID, prompt, opts)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				answer := "❌ Run stopped: " + ctxErr.Error()
				finishErr := r.finish(msg, cardID, &platform.Card{
					Status: platform.CardError,
					Title:  cardTitleFailed,
					Body:   answer,
					Footer: cardFooter(modelName, time.Since(startedAt), nil, r.cfg.ContextWindow),
				}, answer)
				return errors.Join(ctxErr, finishErr)
			}
			sessionNotice = "⚠️ Previous session could not be resumed; started a new session."
			r.logger.Warn("resume failed; starting a new session", "session", sessionID, "error", err)
			if deleteErr := r.sessions.Delete(key); deleteErr != nil {
				r.logger.Warn("could not remove stale session", "error", deleteErr)
			}
			events, err = r.engine.Start(ctx, prompt, opts)
		}
	} else {
		events, err = r.engine.Start(ctx, prompt, opts)
	}
	if err != nil {
		answer := "❌ Couldn't wake the agent: " + err.Error()
		if sessionNotice != "" {
			answer = sessionNotice + "\n\n" + answer
		}
		finishErr := r.finish(msg, cardID, &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, time.Since(startedAt), nil, r.cfg.ContextWindow),
		}, answer)
		return errors.Join(err, finishErr)
	}

	var stream strings.Builder
	var finalMessage string
	var lastAgentMessage string
	var explicitFinal string
	var errTexts []string
	var toolErrors []string
	var usage *tokenUsage
	contextWindow := r.cfg.ContextWindow
	toolCount := 0
	displayBody := ""
	var lastCardUpdate time.Time

	updateProgress := func(body string, force bool) {
		if cardID == "" {
			return
		}
		if !force && time.Since(lastCardUpdate) < r.cfg.CardUpdateInterval {
			return
		}
		activity := ""
		if toolCount > 0 {
			activity = fmt.Sprintf("⏳ Tool calls · %d", toolCount)
		}
		card := &platform.Card{
			Status:   platform.CardThinking,
			Title:    cardTitleWorking,
			Activity: activity,
			Body:     body,
			Footer:   cardFooter(modelName, time.Since(startedAt), nil, r.cfg.ContextWindow),
		}
		if err := r.platform.Update(r.ctx, cardID, platform.Reply{
			ChatID:   msg.ChatID,
			ChatType: msg.ChatType,
			UserID:   msg.UserID,
			ThreadID: msg.ThreadID,
			Card:     card,
		}); err != nil {
			r.logger.Debug("update progress card failed", "error", err)
		} else {
			lastCardUpdate = time.Now()
		}
	}

	for ev := range events {
		switch ev.Type {
		case engine.EventSessionStarted:
			if ev.SessionID != "" {
				if err := r.sessions.Set(key, ev.SessionID); err != nil {
					r.logger.Warn("could not persist session", "error", err)
				}
			}
		case engine.EventMessage:
			lastAgentMessage = ev.Text
			phase, _ := ev.Meta["phase"].(string)
			if phase == "final_answer" {
				explicitFinal = ev.Text
			}
			if phase != "commentary" {
				finalMessage = ev.Text
			}
			displayBody = ev.Text
			updateProgress(displayBody, true)
		case engine.EventTextDelta:
			stream.WriteString(ev.Text)
			lastAgentMessage = stream.String()
			displayBody = stream.String()
			updateProgress(displayBody, false)
		case engine.EventToolCall:
			r.logger.Debug("agent tool call", "chat", msg.ChatID, "command", ev.Text)
			toolCount++
			updateProgress(displayBody, true)
		case engine.EventToolResult:
			r.logger.Debug("agent tool result", "chat", msg.ChatID, "meta", ev.Meta)
			if status, _ := ev.Meta["status"].(string); status == "error" {
				toolErrors = append(toolErrors, ev.Text)
			}
		case engine.EventError:
			if strings.TrimSpace(ev.Text) != "" {
				errTexts = append(errTexts, ev.Text)
			}
		case engine.EventDone:
			usage = parseUsage(ev.Meta)
			if value := metaInt(ev.Meta, "context_window"); value > 0 {
				contextWindow = value
			}
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		errTexts = append(errTexts, ctxErr.Error())
	}
	if err := r.sessions.SetUsage(key, usage, contextWindow); err != nil {
		r.logger.Warn("could not persist session usage", "error", err)
	}

	// Codex exec does not reliably mark the final answer in JSONL. Prefer an
	// explicit final_answer phase, then the last non-commentary message, then
	// the last streamed/received message.
	answer := strings.TrimSpace(explicitFinal)
	if answer == "" {
		answer = strings.TrimSpace(finalMessage)
	}
	if answer == "" {
		answer = strings.TrimSpace(stream.String())
	}
	if answer == "" {
		answer = strings.TrimSpace(lastAgentMessage)
	}
	if answer == "" {
		if len(errTexts) > 0 {
			answer = "❌ " + strings.Join(errTexts, "\n")
		} else if len(toolErrors) > 0 {
			answer = "❌ " + strings.Join(toolErrors, "\n")
		} else {
			answer = "(Agent returned no text output)"
		}
	} else if len(errTexts) > 0 {
		answer += "\n\n⚠️ " + strings.Join(errTexts, "\n")
	}
	if sessionNotice != "" {
		answer = sessionNotice + "\n\n" + answer
	}
	if len(answer) > maxReplyBytes {
		answer = execguard.Truncate(answer, maxReplyBytes) + "\n\n… (output truncated)"
	}

	elapsed := time.Since(startedAt)
	failed := len(errTexts) > 0 || (len(toolErrors) > 0 && len(finalMessage) == 0 && stream.Len() == 0 && lastAgentMessage == "")
	var finishErr error
	if failed {
		finishErr = r.finish(msg, cardID, &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, elapsed, usage, contextWindow),
		}, answer)
	} else {
		finishErr = r.finish(msg, cardID, &platform.Card{
			Status: platform.CardComplete,
			Title:  cardTitleComplete,
			Body:   answer,
			Footer: cardFooter(modelName, elapsed, usage, contextWindow),
		}, answer)
	}

	if len(errTexts) > 0 && ctx.Err() == nil {
		r.recordLesson(msg, lessonPrompt, strings.Join(errTexts, "\n"), workdir)
	}
	var runErr error
	if len(errTexts) > 0 {
		runErr = fmt.Errorf("agent error: %s", execguard.Truncate(strings.Join(errTexts, "\n"), 4<<10))
	} else if failed {
		runErr = fmt.Errorf("agent tool error: %s", execguard.Truncate(strings.Join(toolErrors, "\n"), 4<<10))
	}
	return errors.Join(runErr, finishErr)
}

func initialCardBody(hasAttachments bool) string {
	if hasAttachments {
		return "Gathering your attachments…"
	}
	return "Hold on, warming up the hive…"
}

func (r *Router) finish(msg platform.Message, cardID string, card *platform.Card, fallbackText string) error {
	if cardID != "" {
		if err := r.platform.Update(r.ctx, cardID, platform.Reply{
			ChatID:   msg.ChatID,
			ChatType: msg.ChatType,
			UserID:   msg.UserID,
			ThreadID: msg.ThreadID,
			Card:     card,
		}); err == nil {
			return nil
		} else {
			r.logger.Error("update final card failed", "message_id", cardID, "error", err)
			fallback := replyTarget(msg)
			fallback.Card = card
			if _, sendErr := r.platform.Send(r.ctx, fallback); sendErr == nil {
				return nil
			} else {
				r.logger.Error("send final card failed", "chat", msg.ChatID, "error", sendErr)
			}
		}
	}
	return r.reply(msg, fallbackText)
}

func (r *Router) stop(key SessionKey, msg platform.Message) {
	sessionID, ok := r.sessions.Get(key)
	if !ok {
		r.sendCommandResult(msg, platform.CardInfo, "✋ Stop", "No agent is running in this session.")
		return
	}
	if err := r.engine.Cancel(r.ctx, sessionID); err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Stop Failed", "Failed to stop: "+err.Error())
		return
	}
	r.sendCommandResult(msg, platform.CardInfo, "✋ Stop Requested", "Stop requested.")
}

func (r *Router) status(key SessionKey) string {
	sessionID, ok := r.sessions.Get(key)
	if !ok {
		sessionID = "New session (not started)"
	}
	lines := []string{
		fmt.Sprintf("Engine: %s", r.engine.Name()),
		fmt.Sprintf("Model: %s", r.resolvedModel()),
		fmt.Sprintf("Session: %s", sessionID),
	}
	if usage, contextWindow, ok := r.sessions.Usage(key); ok {
		if line := formatContextUsage(&usage, contextWindow); line != "" {
			lines = append(lines, "Context: "+line)
		}
		if line := formatTurnUsage(&usage); line != "" {
			lines = append(lines, "Last turn: "+line)
		}
	}
	return strings.Join(lines, "\n")
}

func (r *Router) withIdentity(prompt string) string {
	return r.identityBrief() + "\n\n" + prompt
}

func (r *Router) identityBrief() string {
	name := r.cfg.BotName
	if name == "" {
		name = "Nectar"
	}
	version := r.cfg.Version
	if version == "" {
		version = "dev"
	}
	engineName := engineDisplayName(r.engine.Name())
	modelName := r.resolvedModel()
	return fmt.Sprintf(`## Nectar runtime
You are %s, the assistant provided by Nectar v%s. Nectar is a self-hosted
bridge that connects chat platforms to local coding-agent CLIs.

Current platform: %s
Underlying engine: %s
Current model: %s

When asked who you are, answer as %s and mention the underlying engine/model
only as the execution backend. Do not claim to be Codex, Claude, OpenCode,
Cursor, or Pi itself. Do not say you cannot access Nectar; you are running
inside it. If asked what model powers you, answer with the underlying model.

Nectar capabilities: coding tasks, persistent sessions, scheduled tasks, and
long-term memory. Use the `+"`\"${NECTAR_CLI:-nectar}\" schedule`"+` and
`+"`\"${NECTAR_CLI:-nectar}\" memory`"+` CLIs when appropriate.`,
		name, version, r.platform.Name(), engineName, modelName, name)
}

func (r *Router) aboutText() string {
	name := r.cfg.BotName
	if name == "" {
		name = "Nectar"
	}
	version := r.cfg.Version
	if version == "" {
		version = "dev"
	}
	return strings.Join([]string{
		fmt.Sprintf("%s v%s", name, version),
		"A self-hosted bridge between chat platforms and coding-agent CLIs.",
		"",
		"Platform: " + r.platform.Name(),
		"Engine: " + engineDisplayName(r.engine.Name()),
		"Model: " + r.resolvedModel(),
		"",
		"I can run coding tasks, keep sessions, manage scheduled tasks, and use long-term memory.",
		"Use /help for commands.",
	}, "\n")
}

func (r *Router) resolvedModel() string {
	model := r.cfg.ModelName
	if model == "" {
		model = r.cfg.EngineOptions.Model
	}
	if model == "" {
		model = r.engine.Name()
	}
	return model
}

func (r *Router) reply(msg platform.Message, text string) error {
	reply := replyTarget(msg)
	reply.Text = text
	_, err := r.platform.Send(r.ctx, reply)
	if err != nil {
		r.logger.Error("send reply failed", "chat", msg.ChatID, "error", err)
	}
	return err
}

func (r *Router) sendHelp(msg platform.Message) {
	engineName := engineDisplayName(r.engine.Name())
	r.sendCardOrText(msg, helpCard(engineName), helpText(engineName))
}

func (r *Router) sendCommandResult(msg platform.Message, status platform.CardStatus, title, body string) {
	r.sendCardOrText(msg, &platform.Card{
		Status: status,
		Title:  title,
		Body:   body,
	}, body)
}

func (r *Router) sendCardOrText(msg platform.Message, card *platform.Card, fallback string) {
	if caps := r.platform.Capabilities(); caps.Card {
		reply := replyTarget(msg)
		reply.Card = card
		if _, err := r.platform.Send(r.ctx, reply); err == nil {
			return
		} else {
			r.logger.Error("send command card failed", "chat", msg.ChatID, "title", card.Title, "error", err)
		}
	}
	r.reply(msg, fallback)
}

func replyTarget(msg platform.Message) platform.Reply {
	reply := platform.Reply{
		ChatID:       msg.ChatID,
		ChatType:     msg.ChatType,
		UserID:       msg.UserID,
		ReplyContext: msg.ReplyContext,
	}
	if msg.ThreadID != "" {
		reply.ReplyTo = msg.MessageID
		reply.ThreadID = msg.ThreadID
		reply.ReplyInThread = true
	}
	return reply
}

func helpCard(engineName string) *platform.Card {
	return &platform.Card{
		Status: platform.CardInfo,
		Title:  "🍯 Nectar Commands",
		Body:   helpCardBody(engineName),
		Footer: "Send any other message to run " + engineName + ".",
	}
}

func isHelpRequest(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = strings.Trim(normalized, " \t\r\n。！？!?.,，;；:：")
	if normalized == "" {
		return false
	}

	exact := []string{
		"/help", "/commands", "help", "commands",
		"帮助", "命令", "指令", "用法", "怎么用", "如何使用",
		"有什么命令", "有哪些命令", "有什么指令", "有哪些指令",
		"命令列表", "指令列表", "命令有哪些", "指令有哪些", "可用命令",
		"你能做什么", "你可以做什么", "你会什么", "能做什么", "nectar能做什么",
		"怎么使用", "有哪些功能", "有什么功能", "命令帮助",
		"what can you do", "what commands", "what commands are available",
		"what commands do you have", "which commands", "list commands",
		"show commands", "show me the commands", "command list",
		"commands list", "how to use", "what can nectar do",
	}
	for _, item := range exact {
		if normalized == item {
			return true
		}
	}

	if len([]rune(normalized)) > 40 {
		return false
	}
	contains := []string{
		"有哪些命令", "有哪些指令", "有什么命令", "有什么指令",
		"命令列表", "指令列表", "命令有哪些", "指令有哪些",
		"可用命令", "支持哪些命令", "支持哪些指令", "nectar命令", "nectar有哪些命令",
		"what commands does nectar", "nectar commands", "nectar help",
	}
	for _, item := range contains {
		if strings.Contains(normalized, item) {
			return true
		}
	}
	return false
}

type tokenUsage struct {
	// InputTokens/CachedInputTokens/OutputTokens describe this turn's actual
	// consumption. ContextTokens is the final model call's context occupancy.
	InputTokens       int
	CachedInputTokens int
	OutputTokens      int
	ContextTokens     int
}

func parseUsage(meta map[string]any) *tokenUsage {
	if meta == nil {
		return nil
	}
	raw, ok := meta["usage"].(string)
	if !ok || raw == "" {
		return nil
	}
	var decoded struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		OutputTokens      int `json:"output_tokens"`
		ContextTokens     int `json:"context_tokens"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	if decoded.InputTokens == 0 && decoded.CachedInputTokens == 0 &&
		decoded.OutputTokens == 0 && decoded.ContextTokens == 0 {
		return nil
	}
	return &tokenUsage{
		InputTokens:       decoded.InputTokens,
		CachedInputTokens: decoded.CachedInputTokens,
		OutputTokens:      decoded.OutputTokens,
		ContextTokens:     decoded.ContextTokens,
	}
}

func metaInt(meta map[string]any, key string) int {
	if meta == nil {
		return 0
	}
	switch value := meta[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func cardFooter(model string, elapsed time.Duration, usage *tokenUsage, contextWindow int) string {
	model = displayModelName(model)
	parts := make([]string, 0, 3)
	if usage != nil {
		if line := formatContextUsage(usage, contextWindow); line != "" {
			parts = append(parts, "ctx: "+line)
		}
	}
	parts = append(parts, model, fmt.Sprintf("%.1fs", elapsed.Seconds()))
	return strings.Join(parts, " · ")
}

func formatContextUsage(usage *tokenUsage, contextWindow int) string {
	if usage == nil || contextWindow <= 0 || usage.ContextTokens <= 0 {
		return ""
	}
	percent := float64(usage.ContextTokens) / float64(contextWindow) * 100
	return fmt.Sprintf("%s/%s (%.1f%%)",
		formatTokens(usage.ContextTokens), formatTokens(contextWindow), percent)
}

func formatTurnUsage(usage *tokenUsage) string {
	if usage == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if usage.InputTokens > 0 || usage.CachedInputTokens > 0 {
		input := "↑" + formatTokens(usage.InputTokens)
		if usage.CachedInputTokens > 0 {
			input += " (+" + formatTokens(usage.CachedInputTokens) + " cached)"
		}
		parts = append(parts, input)
	}
	if usage.OutputTokens > 0 {
		parts = append(parts, "↓"+formatTokens(usage.OutputTokens))
	}
	return strings.Join(parts, " ")
}

func displayModelName(model string) string {
	model = strings.TrimSpace(model)
	if i := strings.LastIndex(model, "/"); i >= 0 && i+1 < len(model) {
		return model[i+1:]
	}
	return model
}

func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		value := float64(n) / 1_000_000
		if n%1_000_000 == 0 {
			return fmt.Sprintf("%.0fM", value)
		}
		return fmt.Sprintf("%.1fM", value)
	case n >= 1000:
		value := float64(n) / 1000
		if n%1000 == 0 {
			return fmt.Sprintf("%.0fk", value)
		}
		return fmt.Sprintf("%.1fk", value)
	default:
		return strconv.Itoa(n)
	}
}

func (r *Router) recordLesson(msg platform.Message, prompt, errText, workdir string) {
	if isTransientError(errText) {
		return
	}
	if r.lessons != nil {
		lesson := comb.Lesson{
			Condition: "When handling: " + execguard.Truncate(prompt, 200),
			Action:    "Error: " + execguard.Truncate(errText, 1000),
			Status:    comb.StatusPending,
			Source:    msg.Channel + "/" + msg.ChatID,
		}
		if err := r.lessons.Record(r.ctx, lesson); err != nil {
			r.logger.Error("record lesson failed", "error", err)
		}
	}
	if r.memories == nil || r.cfg.DisableMemoryAutoLearn {
		return
	}
	content, locality := memory.NormalizeContent(workdir,
		"Task: "+execguard.Truncate(prompt, 300)+"\nFailure: "+execguard.Truncate(errText, 700))
	_, err := r.memories.Put(r.ctx, memory.Memory{
		Kind:       memory.KindLesson,
		Scope:      memory.ScopeChat,
		ScopeID:    memoryChatScope(msg),
		Content:    content,
		Locality:   locality,
		Tags:       []string{"failure"},
		Source:     msg.Channel + "/" + msg.ChatID,
		Confidence: 0.4,
		Status:     memory.StatusCandidate,
	})
	if err != nil {
		r.logger.Error("record memory candidate failed", "error", err)
	}
}

func isTransientError(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"context canceled", "deadline exceeded", "timed out", "timeout",
		"connection reset", "connection refused", "connection closed",
		"broken pipe", "unexpected eof", "eof",
		"429", "too many requests", "rate limit", "overloaded",
		"temporarily unavailable", "service unavailable",
		"401", "403", "unauthorized", "forbidden",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func helpCardBody(engineName string) string {
	return "**Session**\n" +
		"- `/new` / `/reset` — Start a new session\n" +
		"- `/status` — Show engine, model, and session\n" +
		"- `/about` — Show what Nectar is and what it is running on\n" +
		"- `/stop` — Stop the current task\n" +
		"- `/cron` — List or manage scheduled tasks\n" +
		"- `/memory` — Inspect or curate memory\n" +
		"- `/remember <text>` — Save a memory\n" +
		"- `/help` / `/commands` — Show this command card\n\n" +
		"**Usage**\n" +
		"- Regular messages are sent to " + engineName + "\n" +
		"- Group chats require @Bot"
}

func helpText(engineName string) string {
	return `Nectar commands:
/new, /reset     Start a new session
/status          Show engine, model, and session
/about, /whoami  Show what Nectar is and what it is running on
/stop            Stop the current task
/cron            List or manage scheduled tasks
/memory          Inspect or curate memory
/remember        Save a memory
/help, /commands Show this command card

Other messages are sent to ` + engineName + `.`
}

func engineDisplayName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	case "cursor":
		return "Cursor Agent"
	case "claude":
		return "Claude Code"
	case "pi":
		return "Pi"
	case "":
		return "the configured agent"
	default:
		return strings.TrimSpace(name)
	}
}
