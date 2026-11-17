package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	execguard "github.com/zhouyg1992/nectar/internal/exec"
	"github.com/zhouyg1992/nectar/internal/platform"
)

const (
	relayMarkerPrefix       = "\u200b\u200c\u200b\u200c"
	relayMarkerSuffix       = "\u200c\u200b\u200c\u200b"
	relayLegacyMarkerPrefix = "⟦nectar-relay:"
	relayLegacyMarkerSuffix = "⟧"
	relayStateVersion       = 1
)

var relayMarkerAlphabet = [...]rune{'\u200b', '\u200c', '\u200d', '\u2060'}

type RelayOptions struct {
	Enabled             bool
	Echo                string
	MaxGeneration       int
	MaxEnvelopesPerRoot int
	MaxRunsPerRoot      int
	RootTTL             time.Duration
	PingPongWindow      int
	Session             string
	ArtifactMaxBytes    int64
	AllowOperatorSend   bool
	DataDir             string
	Logger              *slog.Logger
}

type RelayBotConfig struct {
	ID           string
	Name         string
	Role         string
	Description  string
	Capabilities []string
	AcceptFrom   []string
	AllowedChats []string
	PromptMode   string
	Router       *Router
}

type RelayArtifact struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

type RelayEnvelope struct {
	ID              string          `json:"id"`
	RootID          string          `json:"root_id"`
	ParentID        string          `json:"parent_id,omitempty"`
	OriginSessionID string          `json:"origin_session_id,omitempty"`
	OriginRunID     string          `json:"origin_run_id,omitempty"`
	Channel         string          `json:"channel"`
	ChatID          string          `json:"chat_id"`
	ChatType        string          `json:"chat_type,omitempty"`
	ThreadID        string          `json:"thread_id,omitempty"`
	From            string          `json:"from"`
	To              string          `json:"to"`
	Gen             int             `json:"gen"`
	Kind            string          `json:"kind"`
	Mode            string          `json:"mode,omitempty"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
	Deadline        *time.Time      `json:"deadline,omitempty"`
	Body            string          `json:"body"`
	Artifacts       []RelayArtifact `json:"artifacts,omitempty"`
	PromptMode      string          `json:"prompt_mode,omitempty"`
	Echo            string          `json:"echo,omitempty"`
	TargetSession   string          `json:"target_session,omitempty"`
	MirrorMessageID string          `json:"mirror_message_id,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	State           string          `json:"state"`
	Error           string          `json:"error,omitempty"`
}

type RelaySendRequest struct {
	To             string          `json:"to"`
	Body           string          `json:"body"`
	Session        string          `json:"session,omitempty"`
	Mode           string          `json:"mode,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	PromptMode     string          `json:"prompt_mode,omitempty"`
	Echo           string          `json:"echo,omitempty"`
	Artifacts      []RelayArtifact `json:"artifacts,omitempty"`
	From           string          `json:"from,omitempty"`
}

type RelayBotInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role,omitempty"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	AcceptFrom   []string `json:"accept_from,omitempty"`
	Busy         bool     `json:"busy"`
}

type RelayRootInfo struct {
	ID              string    `json:"id"`
	OriginBot       string    `json:"origin_bot"`
	OriginSessionID string    `json:"origin_session_id"`
	ChatID          string    `json:"chat_id"`
	CreatedAt       time.Time `json:"created_at"`
	Envelopes       int       `json:"envelopes"`
	Runs            int       `json:"runs"`
	State           string    `json:"state"`
}

type relayRoot struct {
	ID                  string    `json:"id"`
	OriginBot           string    `json:"origin_bot"`
	OriginSessionID     string    `json:"origin_session_id"`
	OriginRunID         string    `json:"origin_run_id,omitempty"`
	Channel             string    `json:"channel"`
	ChatID              string    `json:"chat_id"`
	ChatType            string    `json:"chat_type,omitempty"`
	ThreadID            string    `json:"thread_id,omitempty"`
	OriginPrompt        string    `json:"origin_prompt"`
	OriginWorkspace     string    `json:"origin_workspace,omitempty"`
	OriginWorkspaceMode string    `json:"origin_workspace_mode,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	LastActivity        time.Time `json:"last_activity"`
	Envelopes           int       `json:"envelopes"`
	Runs                int       `json:"runs"`
	State               string    `json:"state"`
}

type relayTokenBinding struct {
	BotID      string
	SessionID  string
	RootID     string
	Gen        int
	Kind       string
	EnvelopeID string
	Channel    string
	ChatID     string
	ChatType   string
	ThreadID   string
}

type relayRun struct {
	RootID        string
	EnvelopeID    string
	SessionID     string
	Gen           int
	Kind          string
	From          string
	Token         string
	PromptMode    string
	Echo          string
	QuietProgress bool
	SuppressFinal bool
	IsOrigin      bool
	Channel       string
	ChatID        string
	ChatType      string
	ThreadID      string
	OriginRunID   string
}

type RunResult struct {
	Answer    string
	SessionID string
}

type relayBot struct {
	ID           string
	Name         string
	Role         string
	Description  string
	Capabilities []string
	AcceptFrom   []string
	AllowedChats []string
	PromptMode   string
	Router       *Router
	Chats        map[string]bool
}

type relayState struct {
	Version   int                       `json:"version"`
	Roots     map[string]*relayRoot     `json:"roots"`
	Envelopes map[string]*RelayEnvelope `json:"envelopes"`
}

type RelayHub struct {
	mu        sync.Mutex
	opts      RelayOptions
	bots      map[string]*relayBot
	roots     map[string]*relayRoot
	envelopes map[string]*RelayEnvelope
	idem      map[string]string
	tokens    map[string]relayTokenBinding
	statePath string
	logger    *slog.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

var (
	ErrRelayDisabled  = errors.New("relay is disabled")
	ErrRelayTarget    = errors.New("relay target is unknown")
	ErrRelayGenLimit  = errors.New("relay generation limit reached")
	ErrRelayBudget    = errors.New("relay budget exhausted")
	ErrRelayRejected  = errors.New("relay request rejected")
	ErrRelayNotInChat = errors.New("target bot is not in this chat")
	ErrRelayDeadlock  = errors.New("relay wait would deadlock")
)

func NewRelayHub(opts RelayOptions) (*RelayHub, error) {
	if opts.Echo == "" {
		opts.Echo = "summary"
	}
	if opts.MaxGeneration <= 0 {
		opts.MaxGeneration = 4
	}
	if opts.MaxEnvelopesPerRoot <= 0 {
		opts.MaxEnvelopesPerRoot = 12
	}
	if opts.MaxRunsPerRoot <= 0 {
		opts.MaxRunsPerRoot = 8
	}
	if opts.RootTTL <= 0 {
		opts.RootTTL = 30 * time.Minute
	}
	if opts.PingPongWindow <= 0 {
		opts.PingPongWindow = 4
	}
	if opts.Session == "" {
		opts.Session = "per_root"
	}
	if opts.ArtifactMaxBytes <= 0 {
		opts.ArtifactMaxBytes = 10 << 20
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	dir := filepath.Join(opts.DataDir, "relay")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create relay data dir: %w", err)
	}
	hub := &RelayHub{
		opts:      opts,
		bots:      make(map[string]*relayBot),
		roots:     make(map[string]*relayRoot),
		envelopes: make(map[string]*RelayEnvelope),
		idem:      make(map[string]string),
		tokens:    make(map[string]relayTokenBinding),
		statePath: filepath.Join(dir, "state.json"),
		logger:    opts.Logger,
	}
	if err := hub.load(); err != nil {
		return nil, err
	}
	return hub, nil
}

func (h *RelayHub) Enabled() bool {
	return h != nil && h.opts.Enabled
}

func (h *RelayHub) SessionMode() string {
	if h == nil {
		return "per_root"
	}
	return h.opts.Session
}

func (h *RelayHub) Register(cfg RelayBotConfig) {
	if h == nil || !h.opts.Enabled || cfg.Router == nil {
		return
	}
	prompt := strings.ToLower(strings.TrimSpace(cfg.PromptMode))
	if prompt == "" {
		prompt = "full"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bots[cfg.ID] = &relayBot{
		ID:           cfg.ID,
		Name:         cfg.Name,
		Role:         cfg.Role,
		Description:  cfg.Description,
		Capabilities: append([]string(nil), cfg.Capabilities...),
		AcceptFrom:   append([]string(nil), cfg.AcceptFrom...),
		AllowedChats: append([]string(nil), cfg.AllowedChats...),
		PromptMode:   prompt,
		Router:       cfg.Router,
		Chats:        make(map[string]bool),
	}
}

func (h *RelayHub) Start(ctx context.Context) error {
	if h == nil || !h.opts.Enabled {
		return nil
	}
	h.mu.Lock()
	child, cancel := context.WithCancel(ctx)
	h.ctx = child
	h.cancel = cancel
	h.pruneLocked(time.Now().UTC())
	pending := h.pendingLocked()
	_ = h.persistLocked()
	h.mu.Unlock()
	for _, env := range pending {
		go h.deliver(env)
	}
	return nil
}

func (h *RelayHub) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.cancel != nil {
		h.cancel()
	}
	_ = h.persistLocked()
	h.mu.Unlock()
}

func (h *RelayHub) IsMirrorText(text string) bool {
	if h == nil || !h.opts.Enabled {
		return false
	}
	if strings.Contains(text, relayLegacyMarkerPrefix) && strings.Contains(text, relayLegacyMarkerSuffix) {
		return true
	}
	start := strings.Index(text, relayMarkerPrefix)
	if start < 0 {
		return false
	}
	return strings.Contains(text[start+len(relayMarkerPrefix):], relayMarkerSuffix)
}

func relayMarkerFor(id string) string {
	var b strings.Builder
	b.WriteString(relayMarkerPrefix)
	for _, value := range []byte(id) {
		for shift := 6; shift >= 0; shift -= 2 {
			b.WriteRune(relayMarkerAlphabet[(value>>shift)&0x03])
		}
	}
	b.WriteString(relayMarkerSuffix)
	return b.String()
}

func (h *RelayHub) NoteChat(botID, chatID string) {
	if h == nil || !h.opts.Enabled {
		return
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return
	}
	h.mu.Lock()
	if bot := h.bots[botID]; bot != nil {
		bot.Chats[chatID] = true
	}
	h.mu.Unlock()
}

func (h *RelayHub) BeginHumanRun(botID, sessionID, runID string, msg platform.Message, prompt string) *relayRun {
	return h.beginOriginRun("human", botID, sessionID, runID, msg, prompt)
}

func (h *RelayHub) BeginScheduledRun(botID, sessionID, runID string, msg platform.Message, prompt string) *relayRun {
	return h.beginOriginRun("schedule", botID, sessionID, runID, msg, prompt)
}

func (h *RelayHub) beginOriginRun(kind, botID, sessionID, runID string, msg platform.Message, prompt string) *relayRun {
	if h == nil || !h.opts.Enabled {
		return nil
	}
	now := time.Now().UTC()
	root := &relayRoot{
		ID:              newRelayID("root_"),
		OriginBot:       botID,
		OriginSessionID: sessionID,
		OriginRunID:     runID,
		Channel:         msg.Channel,
		ChatID:          msg.ChatID,
		ChatType:        msg.ChatType,
		ThreadID:        msg.ThreadID,
		OriginPrompt:    truncateRelayText(prompt, 2000),
		CreatedAt:       now,
		LastActivity:    now,
		State:           "active",
	}
	h.mu.Lock()
	if bot := h.bots[botID]; bot != nil {
		bot.Chats[msg.ChatID] = true
	}
	h.pruneLocked(now)
	h.roots[root.ID] = root
	token, err := h.newTokenLocked(relayTokenBinding{
		BotID:     botID,
		SessionID: sessionID,
		RootID:    root.ID,
		Gen:       0,
		Kind:      kind,
		Channel:   msg.Channel,
		ChatID:    msg.ChatID,
		ChatType:  msg.ChatType,
		ThreadID:  msg.ThreadID,
	})
	if err != nil {
		delete(h.roots, root.ID)
		h.mu.Unlock()
		h.logger.Warn("could not create relay token", "error", err)
		return nil
	}
	_ = h.persistLocked()
	h.mu.Unlock()
	return &relayRun{
		RootID:      root.ID,
		SessionID:   sessionID,
		Gen:         0,
		Kind:        kind,
		Token:       token,
		PromptMode:  "full",
		Echo:        "full",
		IsOrigin:    true,
		Channel:     msg.Channel,
		ChatID:      msg.ChatID,
		ChatType:    msg.ChatType,
		ThreadID:    msg.ThreadID,
		OriginRunID: runID,
	}
}

func (h *RelayHub) SetOriginWorkspace(rootID, workspace, mode string) {
	if h == nil || !h.opts.Enabled || rootID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if root := h.roots[rootID]; root != nil {
		root.OriginWorkspace = workspace
		root.OriginWorkspaceMode = mode
		root.LastActivity = time.Now().UTC()
		_ = h.persistLocked()
	}
}

func (h *RelayHub) FinishRun(run *relayRun) {
	if h == nil || run == nil || run.Token == "" {
		return
	}
	h.mu.Lock()
	delete(h.tokens, run.Token)
	h.mu.Unlock()
}

func (h *RelayHub) BeginTargetRun(env RelayEnvelope, sessionID string) (*relayRun, error) {
	if h == nil || !h.opts.Enabled {
		return nil, ErrRelayDisabled
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	root := h.roots[env.RootID]
	if root == nil {
		return nil, fmt.Errorf("%w: root %s not found", ErrRelayRejected, env.RootID)
	}
	if root.State == "canceled" {
		return nil, fmt.Errorf("%w: root canceled", ErrRelayRejected)
	}
	if root.Runs+1 > h.opts.MaxRunsPerRoot {
		return nil, fmt.Errorf("%w: runs", ErrRelayBudget)
	}
	target := h.bots[env.To]
	promptMode := strings.ToLower(strings.TrimSpace(env.PromptMode))
	if promptMode == "" && target != nil {
		promptMode = target.PromptMode
	}
	if promptMode == "" {
		promptMode = "full"
	}
	echo := strings.ToLower(strings.TrimSpace(env.Echo))
	if echo == "" {
		echo = h.opts.Echo
	}
	run := &relayRun{
		RootID:      env.RootID,
		EnvelopeID:  env.ID,
		SessionID:   sessionID,
		Gen:         env.Gen,
		Kind:        env.Kind,
		From:        env.From,
		PromptMode:  promptMode,
		Echo:        echo,
		Channel:     env.Channel,
		ChatID:      env.ChatID,
		ChatType:    env.ChatType,
		ThreadID:    env.ThreadID,
		OriginRunID: env.OriginRunID,
	}
	if env.Kind != "reply" {
		run.QuietProgress = echo == "summary" || echo == "off"
		run.SuppressFinal = echo == "off"
	}
	token, err := h.newTokenLocked(relayTokenBinding{
		BotID:      env.To,
		SessionID:  sessionID,
		RootID:     env.RootID,
		Gen:        env.Gen,
		Kind:       env.Kind,
		EnvelopeID: env.ID,
		Channel:    env.Channel,
		ChatID:     env.ChatID,
		ChatType:   env.ChatType,
		ThreadID:   env.ThreadID,
	})
	if err != nil {
		return nil, err
	}
	run.Token = token
	root.Runs++
	root.LastActivity = time.Now().UTC()
	_ = h.persistLocked()
	return run, nil
}

func (h *RelayHub) Bots() []RelayBotInfo {
	if h == nil || !h.opts.Enabled {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]RelayBotInfo, 0, len(h.bots))
	for _, bot := range h.bots {
		out = append(out, RelayBotInfo{
			ID:           bot.ID,
			Name:         bot.Name,
			Role:         bot.Role,
			Description:  bot.Description,
			Capabilities: append([]string(nil), bot.Capabilities...),
			AcceptFrom:   append([]string(nil), bot.AcceptFrom...),
			Busy:         bot.Router != nil && bot.Router.Busy(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (h *RelayHub) Roots() []RelayRootInfo {
	if h == nil || !h.opts.Enabled {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]RelayRootInfo, 0, len(h.roots))
	for _, root := range h.roots {
		out = append(out, RelayRootInfo{
			ID:              root.ID,
			OriginBot:       root.OriginBot,
			OriginSessionID: root.OriginSessionID,
			ChatID:          root.ChatID,
			CreatedAt:       root.CreatedAt,
			Envelopes:       root.Envelopes,
			Runs:            root.Runs,
			State:           root.State,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (h *RelayHub) LatestRoot(botID, chatID string) (RelayRootInfo, bool) {
	if h == nil || !h.opts.Enabled {
		return RelayRootInfo{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var latest *relayRoot
	for _, root := range h.roots {
		if root.OriginBot != botID || root.ChatID != chatID || root.State == "canceled" {
			continue
		}
		if latest == nil || root.LastActivity.After(latest.LastActivity) {
			latest = root
		}
	}
	if latest == nil {
		return RelayRootInfo{}, false
	}
	return RelayRootInfo{
		ID:              latest.ID,
		OriginBot:       latest.OriginBot,
		OriginSessionID: latest.OriginSessionID,
		ChatID:          latest.ChatID,
		CreatedAt:       latest.CreatedAt,
		Envelopes:       latest.Envelopes,
		Runs:            latest.Runs,
		State:           latest.State,
	}, true
}

func (h *RelayHub) Root(rootID string) (RelayRootInfo, []RelayEnvelope, bool) {
	if h == nil || !h.opts.Enabled {
		return RelayRootInfo{}, nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	root := h.roots[rootID]
	if root == nil {
		return RelayRootInfo{}, nil, false
	}
	info := RelayRootInfo{
		ID:              root.ID,
		OriginBot:       root.OriginBot,
		OriginSessionID: root.OriginSessionID,
		ChatID:          root.ChatID,
		CreatedAt:       root.CreatedAt,
		Envelopes:       root.Envelopes,
		Runs:            root.Runs,
		State:           root.State,
	}
	envelopes := make([]RelayEnvelope, 0)
	for _, env := range h.envelopes {
		if env.RootID == rootID {
			envelopes = append(envelopes, *env)
		}
	}
	sort.Slice(envelopes, func(i, j int) bool { return envelopes[i].CreatedAt.Before(envelopes[j].CreatedAt) })
	return info, envelopes, true
}

func (h *RelayHub) OriginPrompt(rootID string) string {
	if h == nil || !h.opts.Enabled {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if root := h.roots[rootID]; root != nil {
		return root.OriginPrompt
	}
	return ""
}

func (h *RelayHub) CancelRoot(rootID string) error {
	if h == nil || !h.opts.Enabled {
		return ErrRelayDisabled
	}
	rootID = strings.TrimSpace(rootID)
	h.mu.Lock()
	root := h.roots[rootID]
	if root == nil {
		h.mu.Unlock()
		return fmt.Errorf("relay root %q not found", rootID)
	}
	root.State = "canceled"
	root.LastActivity = time.Now().UTC()
	for _, env := range h.envelopes {
		if env.RootID == rootID && (env.State == "pending" || env.State == "running") {
			env.State = "canceled"
		}
	}
	bots := h.botRoutersLocked()
	_ = h.persistLocked()
	h.mu.Unlock()
	for _, router := range bots {
		router.CancelRelayRoot(rootID)
	}
	return nil
}

func (h *RelayHub) Peers(botID, chatID string) []RelayBotInfo {
	bots := h.Bots()
	out := make([]RelayBotInfo, 0, len(bots))
	for _, bot := range bots {
		if bot.ID != botID {
			out = append(out, bot)
		}
	}
	return out
}

func (h *RelayHub) Send(ctx context.Context, token string, req RelaySendRequest) (RelayEnvelope, bool, error) {
	if h == nil || !h.opts.Enabled {
		return RelayEnvelope{}, false, ErrRelayDisabled
	}
	if strings.EqualFold(strings.TrimSpace(req.Mode), "steer") {
		return RelayEnvelope{}, false, fmt.Errorf("%w: steer mode is not implemented in M0", ErrRelayRejected)
	}
	req.To = strings.TrimSpace(req.To)
	req.Body = strings.TrimSpace(req.Body)
	if req.To == "" || req.Body == "" {
		return RelayEnvelope{}, false, fmt.Errorf("%w: target and body are required", ErrRelayRejected)
	}
	if len(req.Body) > 60_000 {
		return RelayEnvelope{}, false, fmt.Errorf("%w: body is too large (max 60000 bytes)", ErrRelayRejected)
	}

	h.mu.Lock()
	binding, ok := h.tokens[strings.TrimSpace(token)]
	if !ok {
		if !h.opts.AllowOperatorSend {
			h.mu.Unlock()
			return RelayEnvelope{}, false, fmt.Errorf("%w: relay token is invalid or expired", ErrRelayRejected)
		}
		from := strings.TrimSpace(req.From)
		if from == "" {
			h.mu.Unlock()
			return RelayEnvelope{}, false, fmt.Errorf("%w: operator send requires from", ErrRelayRejected)
		}
		binding = relayTokenBinding{BotID: from, RootID: "", Gen: 0, Kind: "operator"}
	}
	from := binding.BotID
	root := h.roots[binding.RootID]
	if root == nil {
		if strings.TrimSpace(binding.RootID) != "" {
			h.mu.Unlock()
			return RelayEnvelope{}, false, fmt.Errorf("%w: root expired", ErrRelayBudget)
		}
		// Operator sends without a root get a fresh one scoped to the target chat.
		root = &relayRoot{
			ID:           newRelayID("root_"),
			OriginBot:    from,
			Channel:      binding.Channel,
			ChatID:       binding.ChatID,
			ChatType:     binding.ChatType,
			ThreadID:     binding.ThreadID,
			CreatedAt:    time.Now().UTC(),
			LastActivity: time.Now().UTC(),
			State:        "active",
		}
		h.roots[root.ID] = root
	}
	if root.State == "canceled" {
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: root canceled", ErrRelayRejected)
	}
	now := time.Now().UTC()
	if now.Sub(root.LastActivity) > h.opts.RootTTL {
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: root expired", ErrRelayBudget)
	}
	target := h.bots[req.To]
	if target == nil {
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: %s", ErrRelayTarget, req.To)
	}
	if from == target.ID {
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: bot cannot relay to itself", ErrRelayRejected)
	}
	if len(target.AcceptFrom) > 0 && !containsString(target.AcceptFrom, from) {
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: %s does not accept relay from %s", ErrRelayRejected, target.ID, from)
	}
	if len(target.AcceptFrom) == 0 && !h.targetInChatLocked(target, root.ChatID) {
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: %s", ErrRelayNotInChat, target.ID)
	}
	idem := strings.TrimSpace(req.IdempotencyKey)
	if idem == "" {
		idem = relayIdempotencyKey(from, target.ID, root.ID, binding.EnvelopeID, "ask", req.Body)
	}
	if existing, ok := h.idem[idem]; ok {
		env := h.envelopes[existing]
		if env != nil {
			copy := *env
			h.mu.Unlock()
			return copy, true, nil
		}
	}
	gen := binding.Gen + 1
	if gen > h.opts.MaxGeneration {
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: gen %d > %d", ErrRelayGenLimit, gen, h.opts.MaxGeneration)
	}
	if root.Envelopes+1 > h.opts.MaxEnvelopesPerRoot {
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: envelopes", ErrRelayBudget)
	}
	echo := strings.ToLower(strings.TrimSpace(req.Echo))
	if echo == "" {
		echo = h.opts.Echo
	}
	switch echo {
	case "full", "summary", "off":
	default:
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: unsupported echo %q", ErrRelayRejected, echo)
	}
	promptMode := strings.ToLower(strings.TrimSpace(req.PromptMode))
	if promptMode == "" {
		promptMode = target.PromptMode
	}
	switch promptMode {
	case "full", "minimal", "none":
	default:
		h.mu.Unlock()
		return RelayEnvelope{}, false, fmt.Errorf("%w: unsupported prompt mode %q", ErrRelayRejected, promptMode)
	}
	env := RelayEnvelope{
		ID:              newRelayID("env_"),
		RootID:          root.ID,
		ParentID:        binding.EnvelopeID,
		OriginSessionID: binding.SessionID,
		OriginRunID:     root.OriginRunID,
		Channel:         root.Channel,
		ChatID:          root.ChatID,
		ChatType:        root.ChatType,
		ThreadID:        root.ThreadID,
		From:            from,
		To:              target.ID,
		Gen:             gen,
		Kind:            "ask",
		Mode:            "queue",
		IdempotencyKey:  idem,
		Body:            req.Body,
		Artifacts:       append([]RelayArtifact(nil), req.Artifacts...),
		PromptMode:      promptMode,
		Echo:            echo,
		TargetSession:   strings.TrimSpace(req.Session),
		CreatedAt:       now,
		State:           "pending",
	}
	root.Envelopes++
	root.LastActivity = now
	stored := env
	h.envelopes[env.ID] = &stored
	h.idem[idem] = env.ID
	_ = h.persistLocked()
	h.mu.Unlock()

	mirrorID := h.mirror(ctx, env)
	if mirrorID != "" {
		h.mu.Lock()
		if stored := h.envelopes[env.ID]; stored != nil {
			stored.MirrorMessageID = mirrorID
			env.MirrorMessageID = mirrorID
			_ = h.persistLocked()
		}
		h.mu.Unlock()
	}
	go h.deliver(env)
	return env, false, nil
}

func (h *RelayHub) deliver(env RelayEnvelope) {
	if h == nil || !h.opts.Enabled {
		return
	}
	h.mu.Lock()
	target := h.bots[env.To]
	stored := h.envelopes[env.ID]
	canceled := stored != nil && stored.State == "canceled"
	ctx := h.ctx
	h.mu.Unlock()
	if canceled {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if target == nil || target.Router == nil {
		h.finishEnvelope(env.ID, "failed", "target bot is unavailable")
		if env.Kind == "ask" {
			h.sendReply(env, "relay failed: target bot is unavailable")
		}
		return
	}
	h.markEnvelope(env.ID, "running", "")
	result, err := target.Router.ExecuteRelay(ctx, env)
	if err != nil {
		h.finishEnvelope(env.ID, "failed", err.Error())
		if env.Kind == "ask" {
			h.sendReply(env, "relay failed: "+err.Error())
		}
		return
	}
	h.finishEnvelope(env.ID, "done", "")
	if env.Kind == "ask" {
		h.sendReply(env, result.Answer)
	}
}

func (h *RelayHub) sendReply(parent RelayEnvelope, answer string) {
	if h == nil || !h.opts.Enabled || parent.OriginSessionID == "" {
		return
	}
	h.mu.Lock()
	origin := h.bots[parent.From]
	root := h.roots[parent.RootID]
	if origin == nil || root == nil || root.State == "canceled" {
		h.mu.Unlock()
		return
	}
	if root.Runs+1 > h.opts.MaxRunsPerRoot {
		h.mu.Unlock()
		h.logger.Warn("relay reply budget exhausted", "root", parent.RootID)
		return
	}
	idem := "reply:" + parent.ID
	if existing, ok := h.idem[idem]; ok {
		if env := h.envelopes[existing]; env != nil {
			h.mu.Unlock()
			return
		}
	}
	now := time.Now().UTC()
	body := truncateRelayText(strings.TrimSpace(answer), 60_000)
	if body == "" {
		body = "(target returned no text output)"
	}
	env := RelayEnvelope{
		ID:              newRelayID("env_"),
		RootID:          parent.RootID,
		ParentID:        parent.ID,
		OriginSessionID: parent.OriginSessionID,
		OriginRunID:     parent.OriginRunID,
		Channel:         parent.Channel,
		ChatID:          parent.ChatID,
		ChatType:        parent.ChatType,
		ThreadID:        parent.ThreadID,
		From:            parent.To,
		To:              parent.From,
		Gen:             parent.Gen + 1,
		Kind:            "reply",
		Mode:            "queue",
		IdempotencyKey:  idem,
		Body:            body,
		PromptMode:      "full",
		Echo:            "full",
		CreatedAt:       now,
		State:           "pending",
	}
	root.Envelopes++
	root.LastActivity = now
	stored := env
	h.envelopes[env.ID] = &stored
	h.idem[idem] = env.ID
	_ = h.persistLocked()
	h.mu.Unlock()
	go h.deliver(env)
}

func (h *RelayHub) markEnvelope(id, state, errText string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if env := h.envelopes[id]; env != nil {
		if env.State == "canceled" && state != "canceled" {
			return
		}
		env.State = state
		env.Error = errText
		if root := h.roots[env.RootID]; root != nil {
			root.LastActivity = time.Now().UTC()
		}
		_ = h.persistLocked()
	}
}

func (h *RelayHub) finishEnvelope(id, state, errText string) {
	h.markEnvelope(id, state, errText)
}

func (h *RelayHub) targetInChatLocked(target *relayBot, chatID string) bool {
	if chatID == "" {
		return false
	}
	if target.Chats[chatID] {
		return true
	}
	for _, allowed := range target.AllowedChats {
		if allowed == chatID {
			return true
		}
	}
	return false
}

func (h *RelayHub) botRoutersLocked() []*Router {
	out := make([]*Router, 0, len(h.bots))
	for _, bot := range h.bots {
		if bot.Router != nil {
			out = append(out, bot.Router)
		}
	}
	return out
}

func (h *RelayHub) newTokenLocked(binding relayTokenBinding) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	h.tokens[token] = binding
	return token, nil
}

func (h *RelayHub) mirror(ctx context.Context, env RelayEnvelope) string {
	if env.Echo == "off" {
		return ""
	}
	h.mu.Lock()
	source := h.bots[env.From]
	target := h.bots[env.To]
	h.mu.Unlock()
	if source == nil || source.Router == nil || target == nil {
		return ""
	}
	id, err := source.Router.SendRelayMirror(ctx, env, source, target)
	if err != nil {
		h.logger.Warn("send relay mirror failed", "root", env.RootID, "error", err)
		return ""
	}
	return id
}

func (h *RelayHub) load() error {
	data, err := os.ReadFile(h.statePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read relay state: %w", err)
	}
	var state relayState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse relay state: %w", err)
	}
	if state.Version > relayStateVersion {
		return fmt.Errorf("unsupported relay state version %d", state.Version)
	}
	if state.Roots != nil {
		h.roots = state.Roots
	}
	if state.Envelopes != nil {
		h.envelopes = state.Envelopes
	}
	for _, env := range h.envelopes {
		if env.State == "running" {
			env.State = "failed"
			env.Error = "daemon restarted while the relay run was active"
		}
		if env.IdempotencyKey != "" {
			h.idem[env.IdempotencyKey] = env.ID
		}
	}
	return nil
}

func (h *RelayHub) persistLocked() error {
	state := relayState{
		Version:   relayStateVersion,
		Roots:     h.roots,
		Envelopes: h.envelopes,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode relay state: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(h.statePath)
	temp, err := os.CreateTemp(dir, ".relay-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create relay state temp: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, h.statePath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (h *RelayHub) pendingLocked() []RelayEnvelope {
	out := make([]RelayEnvelope, 0)
	for _, env := range h.envelopes {
		if env.State == "pending" {
			out = append(out, *env)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (h *RelayHub) pruneLocked(now time.Time) {
	cutoff := now.Add(-h.opts.RootTTL)
	for id, root := range h.roots {
		if root.LastActivity.Before(cutoff) {
			delete(h.roots, id)
			for envID, env := range h.envelopes {
				if env.RootID == id {
					delete(h.envelopes, envID)
					if env.IdempotencyKey != "" {
						delete(h.idem, env.IdempotencyKey)
					}
				}
			}
		}
	}
}

func (h *RelayHub) SendRequestError(err error) int {
	switch {
	case errors.Is(err, ErrRelayDisabled):
		return 503
	case errors.Is(err, ErrRelayRejected), errors.Is(err, ErrRelayNotInChat):
		return 403
	case errors.Is(err, ErrRelayTarget):
		return 404
	case errors.Is(err, ErrRelayGenLimit), errors.Is(err, ErrRelayBudget):
		return 429
	default:
		return 400
	}
}

func relayErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrRelayDisabled):
		return "ERR_DISABLED"
	case errors.Is(err, ErrRelayGenLimit):
		return "ERR_GEN_LIMIT"
	case errors.Is(err, ErrRelayBudget):
		return "ERR_BUDGET"
	case errors.Is(err, ErrRelayTarget):
		return "ERR_TARGET_UNKNOWN"
	case errors.Is(err, ErrRelayNotInChat):
		return "ERR_NOT_IN_CHAT"
	case errors.Is(err, ErrRelayRejected):
		return "ERR_REJECTED"
	case errors.Is(err, ErrRelayDeadlock):
		return "ERR_DEADLOCK"
	default:
		return "ERR_INTERNAL"
	}
}

func newRelayID(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return prefix + hex.EncodeToString(raw[:])
}

func relayIdempotencyKey(from, to, root, parent, kind, body string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{from, to, root, parent, kind, body}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func truncateRelayText(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return execguard.Truncate(text, max) + "\n\n… (truncated)"
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}
