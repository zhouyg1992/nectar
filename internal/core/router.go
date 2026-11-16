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
	"sync/atomic"
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
	BotID                  string
	AgentID                string
	BotName                string
	Version                string
	ModelName              string
	ContextWindow          int
	CardUpdateInterval     time.Duration
	QueueNoticeDelay       time.Duration
	EngineOptions          engine.EngineOptions
	AdminUsers             []string
	DataDir                string
	APISocket              string
	WorkspaceLocks         *execguard.WorkspaceLocks
	MemoryMaxItems         int
	MemoryMaxChars         int
	MemoryDefaultScope     string
	DisableMemoryAutoLearn bool
}

type sessionRun struct {
	sessionID       string
	key             SessionKey
	cancel          context.CancelFunc
	engineSessionID string
	notice          *queuedRunNotice
}

type queuedRunNotice struct {
	mu        sync.Mutex
	msg       platform.Message
	ahead     int
	cardID    string
	sent      bool
	started   bool
	canceled  bool
	startedCh chan struct{}
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

	runMu  sync.Mutex
	runs   map[string]*sessionRun
	runSeq atomic.Uint64
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
	if cfg.BotID == "" {
		cfg.BotID = cfg.AgentID
	}
	if cfg.WorkspaceLocks == nil {
		cfg.WorkspaceLocks = execguard.NewWorkspaceLocks()
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
	if cfg.QueueNoticeDelay <= 0 {
		cfg.QueueNoticeDelay = 800 * time.Millisecond
	}
	if cfg.MemoryMaxItems <= 0 {
		cfg.MemoryMaxItems = 6
	}
	if cfg.MemoryMaxChars <= 0 {
		cfg.MemoryMaxChars = 1500
	}
	if strings.TrimSpace(cfg.MemoryDefaultScope) == "" {
		cfg.MemoryDefaultScope = "bot"
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
		runs:       make(map[string]*sessionRun),
	}
}

func (r *Router) SetScheduling(anchors *AnchorStore, scheduler *Scheduler) {
	r.anchors = anchors
	r.scheduler = scheduler
}

func (r *Router) lockWorkspace(path string, msg platform.Message) func() {
	key := path + "\x00" + msg.Channel + "/" + msg.ChatID + "/" + msg.ThreadID
	return r.cfg.WorkspaceLocks.Lock(key)
}

func (r *Router) newRunID(sessionID string) string {
	return fmt.Sprintf("%s:%d", sessionID, r.runSeq.Add(1))
}

func (r *Router) trackRun(runID, sessionID string, key SessionKey, cancel context.CancelFunc, notice *queuedRunNotice) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	r.runs[runID] = &sessionRun{sessionID: sessionID, key: key, cancel: cancel, notice: notice}
}

func (r *Router) untrackRun(runID string) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	delete(r.runs, runID)
}

func (r *Router) setRunEngineSession(runID, engineSessionID string) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	if run := r.runs[runID]; run != nil {
		run.engineSessionID = engineSessionID
	}
}

func (r *Router) runningForKey(key SessionKey) []sessionRun {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	out := make([]sessionRun, 0, len(r.runs))
	for _, run := range r.runs {
		if run.key == key {
			out = append(out, *run)
		}
	}
	return out
}

func (r *Router) isSessionRunning(sessionID string) bool {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	for _, run := range r.runs {
		if run.sessionID == sessionID {
			return true
		}
	}
	return false
}

func newQueuedRunNotice(msg platform.Message) *queuedRunNotice {
	return &queuedRunNotice{
		msg:       msg,
		startedCh: make(chan struct{}),
	}
}

func (n *queuedRunNotice) setAhead(ahead int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ahead = ahead
}

func (n *queuedRunNotice) markStarted() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.started {
		return n.cardID
	}
	n.started = true
	close(n.startedCh)
	return n.cardID
}

func (r *Router) sendQueuedNoticeAfter(ctx context.Context, notice *queuedRunNotice) {
	timer := time.NewTimer(r.cfg.QueueNoticeDelay)
	defer timer.Stop()
	select {
	case <-notice.startedCh:
		return
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	notice.mu.Lock()
	defer notice.mu.Unlock()
	if notice.started || notice.canceled || notice.sent {
		return
	}
	noun := "tasks"
	if notice.ahead == 1 {
		noun = "task"
	}
	card := &platform.Card{
		Status: platform.CardThinking,
		Title:  r.queuedCardTitle(),
		Body:   fmt.Sprintf("%d %s ahead. I'll start after the current run finishes.", notice.ahead, noun),
	}
	id, err := r.sendCardReply(notice.msg, card)
	if err != nil {
		r.logger.Debug("send queued notice failed", "error", err)
		return
	}
	notice.sent = true
	notice.cardID = id
}

func (r *Router) queuedCardTitle() string {
	if r.platform.Name() == "dingtalk" {
		return "🕒 Queued"
	}
	return "⏳ Queued"
}

func (r *Router) cancelQueuedNotice(notice *queuedRunNotice) {
	if notice == nil {
		return
	}
	notice.mu.Lock()
	if notice.started || notice.canceled {
		notice.mu.Unlock()
		return
	}
	notice.canceled = true
	cardID := notice.cardID
	msg := notice.msg
	notice.mu.Unlock()
	if cardID == "" {
		return
	}
	reply := replyTarget(msg)
	reply.Card = &platform.Card{
		Status: platform.CardError,
		Title:  "⚠️ Cancelled",
		Body:   "Cancelled while queued.",
	}
	if err := r.platform.Update(r.ctx, cardID, reply); err != nil {
		r.logger.Debug("cancel queued notice failed", "error", err)
	}
}

func (r *Router) sendCardReply(msg platform.Message, card *platform.Card) (string, error) {
	if caps := r.platform.Capabilities(); caps.Card {
		reply := replyTarget(msg)
		reply.Card = card
		return r.platform.Send(r.ctx, reply)
	}
	fallback := strings.TrimSpace(card.Title + "\n\n" + card.Body)
	return "", r.reply(msg, fallback)
}

func (r *Router) Handle(msg platform.Message) {
	key := SessionKey{
		BotID:    r.cfg.BotID,
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		ThreadID: msg.ThreadID,
		AgentID:  r.cfg.AgentID,
	}.normalized()
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
		case "/status":
			if !r.canManageSessions(msg) {
				r.sendCommandResult(msg, platform.CardError, "⚠️ Admin Only", "Only configured admin users can inspect sessions in group chats.")
				return
			}
			r.sendCommandResult(msg, platform.CardInfo, "🌿 Session Status", r.status(key))
			return
		case "/about", "/whoami":
			r.sendCommandResult(msg, platform.CardInfo, "🍯 About "+r.cfg.BotName, r.aboutText())
			return
		case "/stop":
			if !r.canManageSessions(msg) {
				r.sendCommandResult(msg, platform.CardError, "⚠️ Admin Only", "Only configured admin users can stop runs in group chats.")
				return
			}
			r.stop(key, msg)
			return
		}

		if r.handleSteerCommand(key, msg, text) {
			return
		}

		if r.handleSessionCommand(key, msg, text) {
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
	// actual agent run is queued and serialized per Nectar session.
	session, err := r.sessions.EnsureActive(key, "")
	if err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Session Error", "Could not start a session: "+err.Error())
		return
	}
	runCtx, cancel := context.WithCancel(r.ctx)
	runID := r.newRunID(session.ID)
	notice := newQueuedRunNotice(msg)
	r.trackRun(runID, session.ID, key, cancel, notice)
	ahead := r.queue.Do(session.ID, func() {
		defer cancel()
		defer r.untrackRun(runID)
		r.execute(runCtx, key, session.ID, runID, msg, text, notice)
	})
	if ahead > 0 {
		notice.setAhead(ahead)
		go r.sendQueuedNoticeAfter(runCtx, notice)
	}
}

// ExecuteScheduled runs a scheduler-owned prompt through the same per-session
// queue as chat messages and waits until that turn finishes. The scheduler uses
// the returned error to report the actual agent result instead of merely
// whether the prompt was enqueued.
func (r *Router) ExecuteScheduled(ctx context.Context, msg platform.Message, sessionIDs ...string) error {
	msg.Origin = "schedule"
	text := strings.TrimSpace(msg.Text)
	if text == "" && len(msg.Attachments) == 0 {
		return errors.New("scheduled prompt is empty")
	}
	key := SessionKey{
		BotID:    r.cfg.BotID,
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		ThreadID: msg.ThreadID,
		AgentID:  r.cfg.AgentID,
	}.normalized()
	var session Session
	var err error
	if len(sessionIDs) > 0 && strings.TrimSpace(sessionIDs[0]) != "" {
		var ok bool
		session, ok = r.sessions.GetSession(strings.TrimSpace(sessionIDs[0]))
		if !ok || session.Status == SessionStatusArchived || !session.matchesKey(key) {
			return fmt.Errorf("scheduled session %q is not available", sessionIDs[0])
		}
	} else {
		session, err = r.sessions.EnsureActive(key, "")
		if err != nil {
			return fmt.Errorf("start scheduled session: %w", err)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	runID := r.newRunID(session.ID)
	r.trackRun(runID, session.ID, key, cancel, nil)
	done := make(chan error, 1)
	r.queue.Do(session.ID, func() {
		defer cancel()
		defer r.untrackRun(runID)
		if err := runCtx.Err(); err != nil {
			done <- err
			return
		}
		done <- r.execute(runCtx, key, session.ID, runID, msg, text, nil)
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Router) execute(ctx context.Context, key SessionKey, sessionID, runID string, msg platform.Message, prompt string, notice *queuedRunNotice) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session, ok := r.sessions.GetSession(sessionID)
	if !ok {
		return fmt.Errorf("session %q not found", sessionID)
	}
	if err := r.sessions.BeginTurn(sessionID, prompt); err != nil {
		r.logger.Warn("could not begin session turn", "error", err)
	}
	sessionWorkspace, err := r.workspaces.ForSession(session.ID, msg.ChatID)
	if err != nil {
		r.sendCommandResult(msg, platform.CardError, "⚠️ Workspace Error", "❌ Failed to create workspace: "+err.Error())
		return err
	}
	workdir := sessionWorkspace.Path
	projectRoot := sessionWorkspace.ProjectRoot

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
		"NECTAR_PROJECT_ROOT": projectRoot,
		"NECTAR_WORKSPACE":    string(sessionWorkspace.Mode),
	}
	if executable, execErr := os.Executable(); execErr == nil {
		opts.Env["NECTAR_CLI"] = executable
	}
	if key.ThreadID != "" {
		opts.Env["NECTAR_THREAD_ID"] = key.ThreadID
	}
	startedAt := time.Now()
	modelName := r.resolvedModel()
	outbox, err := ensureOutboundDir(workdir)
	if err != nil {
		answer := "❌ Failed to prepare outbound media dir: " + err.Error()
		finishErr := r.finish(msg, "", &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, 0, nil, r.cfg.ContextWindow),
		}, answer)
		return errors.Join(err, finishErr)
	}
	opts.Env["NECTAR_OUTBOX_DIR"] = outbox

	initialCardID := ""
	if notice != nil {
		initialCardID = notice.markStarted()
	}
	cardID := ""
	caps := r.platform.Capabilities()
	stopTyping := func() {}
	if typing, ok := r.platform.(platform.TypingIndicator); ok {
		if stop := typing.StartTyping(r.ctx, msg.ReplyContext); stop != nil {
			stopTyping = stop
		}
	}
	defer stopTyping()

	if initialCardID != "" {
		initial := &platform.Card{
			Status: platform.CardThinking,
			Title:  cardTitleThinking,
			Body:   initialCardBody(len(msg.Attachments) > 0),
		}
		reply := replyTarget(msg)
		reply.Card = initial
		if err := r.platform.Update(r.ctx, initialCardID, reply); err != nil {
			r.logger.Warn("update queued card failed", "error", err)
		} else {
			cardID = initialCardID
		}
	}
	if cardID == "" && caps.Card && caps.Streaming {
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
	prompt = r.withIdentity(prompt, workdir)
	opts.Attachments = attachments

	engineSessionID := session.EngineSessionID
	hasSession := engineSessionID != ""
	prompt = r.withMemory(ctx, msg, workdir, projectRoot, prompt, sessionID, hasSession)
	unlockWorkspace := r.lockWorkspace(workdir, msg)
	defer unlockWorkspace()
	if err := ctx.Err(); err != nil {
		answer := "❌ Run stopped: " + err.Error()
		finishErr := r.finish(msg, cardID, &platform.Card{
			Status: platform.CardError,
			Title:  cardTitleFailed,
			Body:   answer,
			Footer: cardFooter(modelName, time.Since(startedAt), nil, r.cfg.ContextWindow),
		}, answer)
		return errors.Join(err, finishErr)
	}
	var events <-chan engine.Event
	sessionNotice := ""
	if hasSession {
		events, err = r.engine.Resume(ctx, engineSessionID, prompt, opts)
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
			r.logger.Warn("resume failed; starting a new session", "session", engineSessionID, "error", err)
			if markErr := r.sessions.MarkStale(sessionID); markErr != nil {
				r.logger.Warn("could not mark stale session", "error", markErr)
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
	sawReasoning := false
	sawIntermediateText := false
	var lastCardUpdate time.Time
	finalizing := false

	answerText := func() string {
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
		return answer
	}

	hasAnswer := func() bool {
		return strings.TrimSpace(explicitFinal) != "" ||
			strings.TrimSpace(finalMessage) != "" ||
			stream.Len() > 0 ||
			strings.TrimSpace(lastAgentMessage) != ""
	}

	updateFinalizingCard := func() {
		if cardID == "" || !hasAnswer() || len(errTexts) > 0 {
			return
		}
		finalizing = true
		reply := replyTarget(msg)
		reply.Card = &platform.Card{
			Status: platform.CardFinalizing,
			Title:  cardTitleComplete,
			Body:   answerText(),
			Footer: cardFooter(modelName, time.Since(startedAt), usage, contextWindow),
		}
		if err := r.platform.Update(r.ctx, cardID, reply); err != nil {
			r.logger.Debug("update finalizing card failed", "error", err)
		}
	}

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
				if err := r.sessions.SetEngineSession(sessionID, ev.SessionID); err != nil {
					r.logger.Warn("could not persist session", "error", err)
				}
				r.setRunEngineSession(runID, ev.SessionID)
			}
		case engine.EventMessage:
			sawIntermediateText = true
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
		case engine.EventReasoning:
			sawReasoning = true
			if !sawIntermediateText {
				displayBody = "Thinking through the task…"
				updateProgress(displayBody, true)
			}
		case engine.EventTextDelta:
			sawIntermediateText = true
			stream.WriteString(ev.Text)
			lastAgentMessage = stream.String()
			displayBody = stream.String()
			updateProgress(displayBody, false)
		case engine.EventToolCall:
			r.logger.Debug("agent tool call", "chat", msg.ChatID, "command", ev.Text)
			toolCount++
			if !sawIntermediateText {
				if sawReasoning {
					displayBody = "Thinking through the task…"
				} else {
					displayBody = "Gathering results…"
				}
			}
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
			if parsed := parseUsage(ev.Meta); parsed != nil {
				usage = parsed
			}
			if value := metaInt(ev.Meta, "context_window"); value > 0 {
				contextWindow = value
			}
			updateFinalizingCard()
		case engine.EventUsage:
			if parsed := parseUsage(ev.Meta); parsed != nil {
				usage = parsed
			}
			if value := metaInt(ev.Meta, "context_window"); value > 0 {
				contextWindow = value
			}
			if finalizing {
				updateFinalizingCard()
			}
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		errTexts = append(errTexts, ctxErr.Error())
	}
	if err := r.sessions.FinishTurn(sessionID, usage, contextWindow); err != nil {
		r.logger.Warn("could not finish session turn", "error", err)
	}

	answer := answerText()
	outboundMedia := collectOutboundMedia(outbox, r.logger)

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
	var mediaErr error
	if mediaErr = r.sendOutboundMedia(msg, outboundMedia); mediaErr != nil {
		r.logger.Error("send outbound media failed", "chat", msg.ChatID, "error", mediaErr)
		_ = r.reply(msg, "⚠️ 附件发送失败: "+mediaErr.Error())
	}

	if len(errTexts) > 0 && ctx.Err() == nil {
		r.recordLesson(msg, lessonPrompt, strings.Join(errTexts, "\n"), projectRoot)
	}
	var runErr error
	if len(errTexts) > 0 {
		runErr = fmt.Errorf("agent error: %s", execguard.Truncate(strings.Join(errTexts, "\n"), 4<<10))
	} else if failed {
		runErr = fmt.Errorf("agent tool error: %s", execguard.Truncate(strings.Join(toolErrors, "\n"), 4<<10))
	}
	return errors.Join(runErr, finishErr, mediaErr)
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
	runs := r.runningForKey(key)
	if len(runs) == 0 {
		r.sendCommandResult(msg, platform.CardInfo, "✋ Stop", "No agent is running in this session.")
		return
	}
	for _, run := range runs {
		if run.cancel != nil {
			run.cancel()
		}
		r.cancelQueuedNotice(run.notice)
		if run.engineSessionID != "" {
			_ = r.engine.Cancel(r.ctx, run.engineSessionID)
		}
	}
	body := "Stop requested."
	if len(runs) > 1 {
		body = fmt.Sprintf("Stop requested for %d running sessions.", len(runs))
	}
	r.sendCommandResult(msg, platform.CardInfo, "✋ Stop Requested", body)
}

func (r *Router) status(key SessionKey) string {
	session, ok := r.sessions.Active(key)
	rows := [][2]string{
		{"Engine", r.engine.Name()},
		{"Model", r.resolvedModel()},
	}
	if r.supportsSteer() {
		rows = append(rows, [2]string{"Steer", "supported"})
	} else {
		rows = append(rows, [2]string{"Steer", "unsupported"})
	}
	if !ok {
		rows = append(rows, [2]string{"Session", "New session (not started)"})
		return markdownKeyValue(rows)
	}
	engineSession := session.EngineSessionID
	if engineSession == "" {
		rows = append(rows, [2]string{"Session", "New session (not started)"})
	} else {
		rows = append(rows, [2]string{"Session", engineSession})
	}
	rows = append(rows, [2]string{"Nectar session", "`" + session.ID + "` (" + session.DisplayName() + ")"})
	if workspace, err := r.workspaces.SessionPath(session.ID, session.ChatID); err == nil {
		rows = append(rows, [2]string{"Workspace", workspace.Path})
		rows = append(rows, [2]string{"Workspace mode", string(workspace.Mode)})
	}
	if usage, contextWindow, ok := r.sessions.UsageByID(session.ID); ok {
		if line := formatContextUsage(&usage, contextWindow); line != "" {
			rows = append(rows, [2]string{"Context", line})
		}
		if line := formatTurnUsage(&usage); line != "" {
			rows = append(rows, [2]string{"Last turn", line})
		}
	}
	return markdownKeyValue(rows)
}

func (r *Router) withIdentity(prompt, workdir string) string {
	return r.identityBrief(workdir) + "\n\n" + prompt
}

func (r *Router) identityBrief(workdir string) string {
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
`+"`\"${NECTAR_CLI:-nectar}\" memory`"+` CLIs when appropriate.

%s`,
		name, version, r.platform.Name(), engineName, modelName, name,
		outboundInstruction(workdir))
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

func (r *Router) sendOutboundMedia(msg platform.Message, media []platform.Media) error {
	if len(media) == 0 {
		return nil
	}
	var errs []error
	for _, item := range media {
		reply := replyTarget(msg)
		reply.Media = []platform.Media{item}
		if _, err := r.platform.Send(r.ctx, reply); err != nil {
			errs = append(errs, fmt.Errorf("send outbound file %q: %w", item.Name, err))
			continue
		}
		if err := os.Remove(item.Path); err != nil {
			r.logger.Debug("remove sent outbound file failed", "path", item.Path, "error", err)
		}
	}
	return errors.Join(errs...)
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

func (r *Router) recordLesson(msg platform.Message, prompt, errText, projectRoot string) {
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
	content, locality := memory.NormalizeContent(projectRoot,
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
		"- `/new [name]` / `/reset` — Start a new session; previous sessions stay available\n" +
		"- `/list [all] [limit]` — List sessions for this chat\n" +
		"- `/switch <id|#|name>` — Switch the active session\n" +
		"- `/current` — Show the active session\n" +
		"- `/rename <name>` — Rename the active session\n" +
		"- `/archive [id]` — Archive a session\n" +
		"- `/unarchive <id|name>` — Restore an archived session\n" +
		"- `/status` — Show engine, model, and session\n" +
		"- `/about` — Show what Nectar is and what it is running on\n" +
		"- `/stop` — Stop the current task\n" +
		"- `/steer <text>` — Insert text into the running task (aliases: `/append`, `/ps`, `/btw`)\n" +
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
/new [name]      Start a new session (previous sessions stay available)
/list [all] [N]  List sessions (default limit 50)
/switch ID|#|NAME  Switch active session
/current         Show the active session
/rename NAME     Rename the active session
/archive ID|NAME Archive a session
/unarchive ID|NAME Restore an archived session
/status          Show engine, model, and session
/about, /whoami  Show what Nectar is and what it is running on
/stop            Stop the current task
/steer TEXT      Insert text into the running task
                 Aliases: /append, /ps, /btw
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
