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
	cardTitleThinking = "🐝 Thinking..."
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
		r.sendCommandResult(msg, platform.CardInfo, "🔄 New Session", "已开始新会话。")
		return
	case "/status":
		r.sendCommandResult(msg, platform.CardInfo, "📊 Session Status", r.status(key))
		return
	case "/stop":
		r.stop(key, msg)
		return
	}

	if isHelpRequest(text) {
		r.sendHelp(msg)
		return
	}

	if text == "" {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Unsupported Message", "只支持文本消息。")
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
		r.sendCommandResult(msg, platform.CardError, "⚠️ Workspace Error", "❌ 无法创建工作目录: "+err.Error())
		return
	}

	opts := r.cfg.EngineOptions
	opts.WorkingDir = workdir
	startedAt := time.Now()
	modelName := r.cfg.ModelName
	if modelName == "" {
		modelName = opts.Model
	}
	if modelName == "" {
		modelName = r.engine.Name()
	}

	cardID := ""
	if caps := r.platform.Capabilities(); caps.Card && caps.Streaming {
		initial := &platform.Card{
			Status: platform.CardThinking,
			Title:  cardTitleThinking,
			Body:   "正在思考，请稍候…",
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

	sessionID, hasSession := r.sessions.Get(key)
	var events <-chan engine.Event
	if hasSession {
		events, err = r.engine.Resume(r.ctx, sessionID, prompt, opts)
	} else {
		events, err = r.engine.Start(r.ctx, prompt, opts)
	}
	if err != nil {
		answer := "❌ 启动 Agent 失败: " + err.Error()
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
	var lastCardUpdate time.Time

	updateProgress := func(title, body string) {
		if cardID == "" || time.Since(lastCardUpdate) < r.cfg.CardUpdateInterval {
			return
		}
		card := &platform.Card{
			Status: platform.CardThinking,
			Title:  title,
			Body:   body,
			Footer: cardFooter(modelName, time.Since(startedAt), nil, r.cfg.ContextWindow),
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
			updateProgress(cardTitleWorking, ev.Text)
		case engine.EventTextDelta:
			stream.WriteString(ev.Text)
			lastAgentMessage = stream.String()
			updateProgress(cardTitleWorking, stream.String())
		case engine.EventToolCall:
			r.logger.Debug("agent tool call", "chat", msg.ChatID, "command", ev.Text)
			updateProgress(cardTitleWorking, "正在执行：`"+strings.ReplaceAll(truncateOneLine(ev.Text, 200), "`", "'")+"`")
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
		}
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
			answer = "（Agent 没有返回文本输出）"
		}
	} else if len(errTexts) > 0 {
		answer += "\n\n⚠️ " + strings.Join(errTexts, "\n")
	}
	if len(answer) > maxReplyBytes {
		answer = execguard.Truncate(answer, maxReplyBytes) + "\n\n…（输出已截断）"
	}

	elapsed := time.Since(startedAt)
	failed := len(errTexts) > 0 || (len(toolErrors) > 0 && len(finalMessage) == 0 && stream.Len() == 0 && lastAgentMessage == "")
	if failed {
		r.finish(msg, cardID, &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, elapsed, usage, r.cfg.ContextWindow),
		}, answer)
	} else {
		r.finish(msg, cardID, &platform.Card{
			Status: platform.CardComplete,
			Title:  cardTitleComplete,
			Body:   answer,
			Footer: cardFooter(modelName, elapsed, usage, r.cfg.ContextWindow),
		}, answer)
	}

	if len(errTexts) > 0 {
		r.recordLesson(msg, prompt, strings.Join(errTexts, "\n"))
	}
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
		r.sendCommandResult(msg, platform.CardInfo, "⏹ Stop", "当前会话还没有正在运行的 Agent。")
		return
	}
	if err := r.engine.Cancel(r.ctx, sessionID); err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Stop Failed", "停止失败: "+err.Error())
		return
	}
	r.sendCommandResult(msg, platform.CardInfo, "⏹ Stop Requested", "已请求停止当前任务。")
}

func (r *Router) status(key SessionKey) string {
	sessionID, ok := r.sessions.Get(key)
	if !ok {
		return "会话: 新会话（尚未启动）"
	}
	return fmt.Sprintf("会话: %s", sessionID)
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
	reply := platform.Reply{ChatID: msg.ChatID}
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
		Footer: "发送其他消息即可交给 Codex 执行",
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
	InputTokens   int
	OutputTokens  int
	ContextTokens int
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
		InputTokens   int `json:"input_tokens"`
		OutputTokens  int `json:"output_tokens"`
		ContextTokens int `json:"context_tokens"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	if decoded.InputTokens == 0 && decoded.OutputTokens == 0 && decoded.ContextTokens == 0 {
		return nil
	}
	return &tokenUsage{
		InputTokens:   decoded.InputTokens,
		OutputTokens:  decoded.OutputTokens,
		ContextTokens: decoded.ContextTokens,
	}
}

func cardFooter(model string, elapsed time.Duration, usage *tokenUsage, contextWindow int) string {
	model = displayModelName(model)
	parts := make([]string, 0, 5)
	if usage != nil && contextWindow > 0 {
		used := usage.ContextTokens
		if used == 0 {
			used = usage.InputTokens + usage.OutputTokens
		}
		percent := float64(used) / float64(contextWindow) * 100
		parts = append(parts, fmt.Sprintf(
			"ctx: %s/%s (%.1f%%)",
			formatTokens(used), formatTokens(contextWindow), percent,
		))
	}
	parts = append(parts, model, fmt.Sprintf("%.1fs", elapsed.Seconds()))
	if usage != nil {
		if usage.InputTokens > 0 {
			parts = append(parts, "↑"+formatTokens(usage.InputTokens))
		}
		if usage.OutputTokens > 0 {
			parts = append(parts, "↓"+formatTokens(usage.OutputTokens))
		}
	}
	return strings.Join(parts, " · ")
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

func truncateOneLine(value string, max int) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	return execguard.Truncate(value, max)
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

const helpCardBody = "**会话**\n" +
	"- `/new` / `/reset` — 开始新会话\n" +
	"- `/status` — 查看当前会话\n" +
	"- `/stop` — 停止当前任务\n" +
	"- `/help` / `/commands` — 显示这张命令卡片\n\n" +
	"**使用**\n" +
	"- 普通消息会交给 Codex 执行\n" +
	"- 群聊需要 @Bot"

const helpText = `Nectar 命令:
/new、/reset    开始新会话
/status         查看当前会话
/stop           停止当前任务
/help、/commands 显示命令卡片

其他消息会发送给 Codex 执行。`
