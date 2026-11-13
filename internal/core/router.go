package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/zhouyg1992/nectar/internal/core/decision"
	"github.com/zhouyg1992/nectar/internal/engine"
	execguard "github.com/zhouyg1992/nectar/internal/exec"
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
	AgentID            string
	ModelName          string
	ContextWindow      int
	CardUpdateInterval time.Duration
	EngineOptions      engine.EngineOptions
}

type Router struct {
	ctx        context.Context
	platform   platform.PlatformAdapter
	engine     engine.EngineAdapter
	sessions   *SessionStore
	queue      *Queue
	workspaces *execguard.Workspace
	lessons    comb.Store
	decider    decision.DecisionService
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
	decider decision.DecisionService,
	logger *slog.Logger,
	cfg RouterConfig,
) *Router {
	if cfg.AgentID == "" {
		cfg.AgentID = "codex"
	}
	if cfg.CardUpdateInterval <= 0 {
		cfg.CardUpdateInterval = 2 * time.Second
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
		decider:    decider,
		logger:     logger,
		cfg:        cfg,
	}
}

func (r *Router) Handle(msg platform.Message) {
	key := SessionKey{
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		ThreadID: msg.ThreadID,
		AgentID:  r.cfg.AgentID,
	}
	text := strings.TrimSpace(msg.Text)

	switch text {
	case "/new", "/reset":
		r.sessions.Delete(key)
		r.sendCommandResult(msg, platform.CardInfo, "🌱 New Session", "Started a new session.")
		return
	case "/status":
		r.sendCommandResult(msg, platform.CardInfo, "🌿 Session Status", r.status(key))
		return
	case "/stop":
		r.stop(key, msg)
		return
	}

	if isHelpRequest(text) {
		r.sendHelp(msg)
		return
	}

	if text == "" && len(msg.Attachments) == 0 {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Unsupported Message", "Only text, image, or file messages are supported.")
		return
	}

	// Platform callbacks must return fast (Feishu retries after ~3s), so the
	// actual agent run is queued and serialized per session key.
	r.queue.Do(key.String(), func() {
		r.execute(key, msg, text)
	})
}

func (r *Router) execute(key SessionKey, msg platform.Message, prompt string) {
	workdir, err := r.workspaces.ForChat(msg.ChatID)
	if err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Workspace Error", "❌ Failed to create workspace: "+err.Error())
		return
	}

	opts := r.cfg.EngineOptions
	opts.WorkingDir = workdir
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
		r.finish(msg, cardID, &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, time.Since(startedAt), nil, r.cfg.ContextWindow),
		}, answer)
		return
	}
	defer cleanupAttachments()
	lessonPrompt := prompt
	if strings.TrimSpace(lessonPrompt) == "" && len(attachments) > 0 {
		lessonPrompt = defaultAttachmentPrompt(attachments)
	}
	prompt = buildAttachmentPrompt(prompt, attachments)
	opts.Attachments = attachments

	sessionID, hasSession := r.sessions.Get(key)
	var events <-chan engine.Event
	if hasSession {
		events, err = r.engine.Resume(r.ctx, sessionID, prompt, opts)
	} else {
		events, err = r.engine.Start(r.ctx, prompt, opts)
	}
	if err != nil {
		answer := "❌ Couldn't wake the agent: " + err.Error()
		r.finish(msg, cardID, &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, time.Since(startedAt), nil, r.cfg.ContextWindow),
		}, answer)
		return
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
		if err := r.platform.Update(r.ctx, cardID, platform.Reply{Card: card}); err != nil {
			r.logger.Debug("update progress card failed", "error", err)
		} else {
			lastCardUpdate = time.Now()
		}
	}

	for ev := range events {
		switch ev.Type {
		case engine.EventSessionStarted:
			if ev.SessionID != "" {
				r.sessions.Set(key, ev.SessionID)
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
	r.sessions.SetUsage(key, usage, contextWindow)

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
	if len(answer) > maxReplyBytes {
		answer = execguard.Truncate(answer, maxReplyBytes) + "\n\n… (output truncated)"
	}

	elapsed := time.Since(startedAt)
	failed := len(errTexts) > 0 || (len(toolErrors) > 0 && len(finalMessage) == 0 && stream.Len() == 0 && lastAgentMessage == "")
	if failed {
		r.finish(msg, cardID, &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, elapsed, usage, contextWindow),
		}, answer)
	} else {
		r.finish(msg, cardID, &platform.Card{
			Status: platform.CardComplete,
			Title:  cardTitleComplete,
			Body:   answer,
			Footer: cardFooter(modelName, elapsed, usage, contextWindow),
		}, answer)
	}

	if len(errTexts) > 0 {
		r.recordLesson(msg, lessonPrompt, strings.Join(errTexts, "\n"))
	}
}

func initialCardBody(hasAttachments bool) string {
	if hasAttachments {
		return "Gathering your attachments…"
	}
	return "Hold on, warming up the hive…"
}

func (r *Router) finish(msg platform.Message, cardID string, card *platform.Card, fallbackText string) {
	if cardID != "" {
		if err := r.platform.Update(r.ctx, cardID, platform.Reply{Card: card}); err == nil {
			return
		} else {
			r.logger.Error("update final card failed", "message_id", cardID, "error", err)
			fallback := replyTarget(msg)
			fallback.Card = card
			if _, sendErr := r.platform.Send(r.ctx, fallback); sendErr == nil {
				return
			} else {
				r.logger.Error("send final card failed", "chat", msg.ChatID, "error", sendErr)
			}
		}
	}
	r.reply(msg, fallbackText)
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

func (r *Router) reply(msg platform.Message, text string) {
	reply := replyTarget(msg)
	reply.Text = text
	_, err := r.platform.Send(r.ctx, reply)
	if err != nil {
		r.logger.Error("send reply failed", "chat", msg.ChatID, "error", err)
	}
}

func (r *Router) sendHelp(msg platform.Message) {
	r.sendCardOrText(msg, helpCard(), helpText)
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
		ReplyContext: msg.ReplyContext,
	}
	if msg.ThreadID != "" {
		reply.ReplyTo = msg.MessageID
		reply.ThreadID = msg.ThreadID
		reply.ReplyInThread = true
	}
	return reply
}

func helpCard() *platform.Card {
	return &platform.Card{
		Status: platform.CardInfo,
		Title:  "🍯 Nectar Commands",
		Body:   helpCardBody,
		Footer: "Send any other message to run Codex.",
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

func (r *Router) recordLesson(msg platform.Message, prompt, errText string) {
	if r.lessons == nil {
		return
	}
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

const helpCardBody = "**Session**\n" +
	"- `/new` / `/reset` — Start a new session\n" +
	"- `/status` — Show engine, model, and session\n" +
	"- `/stop` — Stop the current task\n" +
	"- `/help` / `/commands` — Show this command card\n\n" +
	"**Usage**\n" +
	"- Regular messages are sent to Codex\n" +
	"- Group chats require @Bot"

const helpText = `Nectar commands:
/new, /reset     Start a new session
/status          Show engine, model, and session
/stop            Stop the current task
/help, /commands Show this command card

Other messages are sent to Codex.`
