package core

import (
	"context"
	"crypto/rand"
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
)

const relayMaxBodyBytes = 32 << 10

// RelayHubOptions configures the process-wide relay bus.
type RelayHubOptions struct {
	DataDir             string
	Echo                string
	MaxGeneration       int
	MaxEnvelopesPerRoot int
	MaxRunsPerRoot      int
	RootTTL             time.Duration
	FirstMentionOnly    bool
	Logger              *slog.Logger
}

// RelayHub holds the roster, run tokens, roots and envelopes. It lives for the
// whole process and is shared by every bot runtime.
type RelayHub struct {
	mu        sync.Mutex
	opts      RelayHubOptions
	logger    *slog.Logger
	path      string
	bots      map[string]*relayBot
	roots     map[string]*RelayRoot
	envelopes map[string]*RelayEnvelope
	tokens    map[string]*relayTokenBinding
	chats     map[string]map[string]struct{}
	runs      map[string]context.CancelFunc
	ctx       context.Context
	cancel    context.CancelFunc
	started   bool
	wg        sync.WaitGroup
}

// NewRelayHub loads persisted relay state. A missing state file is normal; a
// corrupt one is an error so the caller can refuse to start relay rather than
// overwrite the journal.
func NewRelayHub(opts RelayHubOptions) (*RelayHub, error) {
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
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &RelayHub{
		opts:      opts,
		logger:    logger,
		path:      filepath.Join(opts.DataDir, "relay", "state.json"),
		bots:      make(map[string]*relayBot),
		roots:     make(map[string]*RelayRoot),
		envelopes: make(map[string]*RelayEnvelope),
		tokens:    make(map[string]*relayTokenBinding),
		chats:     make(map[string]map[string]struct{}),
		runs:      make(map[string]context.CancelFunc),
		ctx:       ctx,
		cancel:    cancel,
	}
	if err := h.load(); err != nil {
		cancel()
		return nil, err
	}
	return h, nil
}

// Register adds a bot to the roster. It must be called before Start.
func (h *RelayHub) Register(bot RelayPeer, router *Router) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bots[bot.ID] = &relayBot{
		id:           bot.ID,
		name:         bot.Name,
		role:         bot.Role,
		description:  bot.Description,
		capabilities: append([]string(nil), bot.Capabilities...),
		acceptFrom:   append([]string(nil), bot.AcceptFrom...),
		router:       router,
	}
}

// Start resumes the journal: pending envelopes are delivered, running ones are
// failed because their process died and are not retried automatically.
func (h *RelayHub) Start(ctx context.Context) {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	h.started = true
	if ctx != nil {
		h.ctx, h.cancel = context.WithCancel(ctx)
	}
	pending := make([]*RelayEnvelope, 0)
	interrupted := make([]*RelayEnvelope, 0)
	for _, envelope := range h.envelopes {
		switch envelope.State {
		case RelayStatePending:
			pending = append(pending, envelope)
		case RelayStateRunning:
			interrupted = append(interrupted, envelope)
			envelope.State = RelayStateFailed
			envelope.Error = "interrupted by restart"
			envelope.UpdatedAt = time.Now().UTC()
		}
	}
	h.mu.Unlock()

	h.persist()
	for _, envelope := range interrupted {
		if envelope.Kind == RelayKindAsk {
			h.sendReply(envelope, "⚠️ Relay to "+envelope.To+" was interrupted by a daemon restart.")
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt.Before(pending[j].CreatedAt) })
	for _, envelope := range pending {
		h.dispatch(envelope)
	}
}

// Close stops delivery and waits for in-flight envelopes.
func (h *RelayHub) Close() {
	h.cancel()
	h.wg.Wait()
}

// NoteChat records that a bot is known to participate in a chat.
func (h *RelayHub) NoteChat(chatID, botID string) {
	chatID = strings.TrimSpace(chatID)
	botID = strings.TrimSpace(botID)
	if chatID == "" || botID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.noteChatLocked(chatID, botID)
}

func (h *RelayHub) noteChatLocked(chatID, botID string) {
	members := h.chats[chatID]
	if members == nil {
		members = make(map[string]struct{})
		h.chats[chatID] = members
	}
	members[botID] = struct{}{}
}

// MintToken issues a run token bound to identity, chat, root and generation.
// Empty strings mean the run cannot send relay (relay disabled).
func (h *RelayHub) MintToken(req RelayTokenRequest) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf[:])
	req.Prompt = truncateRelayPrompt(req.Prompt)
	if req.OriginMessageID == "" && req.RootID != "" {
		if root := h.roots[req.RootID]; root != nil {
			req.OriginMessageID = root.OriginMessageID
		}
	}
	h.tokens[token] = &relayTokenBinding{RelayTokenRequest: req, Token: token, CreatedAt: time.Now().UTC()}
	return token
}

// RevokeToken drops a run token when its run ends.
func (h *RelayHub) RevokeToken(token string) {
	if token == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.tokens, token)
}

// TokenBinding resolves a bearer token to its binding, or nil.
func (h *RelayHub) TokenBinding(token string) *relayTokenBinding {
	h.mu.Lock()
	defer h.mu.Unlock()
	binding := h.tokens[strings.TrimSpace(token)]
	if binding == nil {
		return nil
	}
	copied := *binding
	return &copied
}

// Echo returns the configured echo level for relay target runs.
func (h *RelayHub) Echo() string {
	return h.opts.Echo
}

// FirstMentionOnly reports whether a group message that mentions several
// configured bots is handled only by the first bot that was mentioned.
func (h *RelayHub) FirstMentionOnly() bool {
	return h.opts.FirstMentionOnly
}

// BotIDByName resolves a mention name to a configured bot, matching the
// display name or the bot id case-insensitively.
func (h *RelayHub) BotIDByName(name string) (string, bool) {
	want := normalizeMentionName(name)
	if want == "" {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, bot := range h.bots {
		if relayNamesMatch(bot.name, name) || relayNamesMatch(bot.id, name) {
			return bot.id, true
		}
	}
	return "", false
}

func normalizeMentionName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), ""))
}

// relayNamesMatch compares mention names with configured bot names. Platform
// display names may carry decorations such as an emoji suffix that the config
// does not, so a containment match is accepted for reasonably long names.
func relayNamesMatch(configured, mentioned string) bool {
	left, right := normalizeMentionName(configured), normalizeMentionName(mentioned)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	short, long := left, right
	if len([]rune(short)) > len([]rune(long)) {
		short, long = long, short
	}
	if len([]rune(short)) < 3 {
		return false
	}
	return strings.Contains(long, short)
}

// Peers lists the roster for a chat, including busy state.
func (h *RelayHub) Peers(chatID string) []RelayPeer {
	h.mu.Lock()
	bots := make([]*relayBot, 0, len(h.bots))
	for _, bot := range h.bots {
		bots = append(bots, bot)
	}
	h.mu.Unlock()
	peers := make([]RelayPeer, 0, len(bots))
	for _, bot := range bots {
		busy := false
		if bot.router != nil {
			busy = bot.router.Busy()
		}
		peers = append(peers, RelayPeer{
			ID:           bot.id,
			Name:         bot.name,
			Role:         bot.role,
			Description:  bot.description,
			Capabilities: append([]string(nil), bot.capabilities...),
			AcceptFrom:   append([]string(nil), bot.acceptFrom...),
			Busy:         busy,
		})
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	return peers
}

// Send validates and journals one relay message, then returns immediately.
// Delivery happens asynchronously so the sending agent can end its turn.
func (h *RelayHub) Send(binding *relayTokenBinding, req RelaySendRequest) (*RelayEnvelope, error) {
	if binding == nil {
		return nil, relayError(ErrRelayUnauthorized, "missing relay token")
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return nil, relayError(ErrRelayRejected, "relay body is empty")
	}
	if len(body) > relayMaxBodyBytes {
		return nil, relayError(ErrRelayRejected, "relay body exceeds %d bytes", relayMaxBodyBytes)
	}
	targetID := strings.TrimSpace(req.To)

	h.mu.Lock()
	target := h.bots[targetID]
	if target == nil {
		h.mu.Unlock()
		return nil, relayError(ErrRelayTargetUnknown, "unknown bot %q", targetID)
	}
	gen := binding.Gen + 1
	if gen > h.opts.MaxGeneration {
		h.mu.Unlock()
		return nil, relayError(ErrRelayGenLimit, "relay chain reached %d hops (max %d)", gen, h.opts.MaxGeneration)
	}
	if len(target.acceptFrom) > 0 && !containsString(target.acceptFrom, binding.BotID) {
		h.mu.Unlock()
		return nil, relayError(ErrRelayRejected, "%s does not accept relay from %s", targetID, binding.BotID)
	}
	chatID := binding.ChatID
	if binding.RootID != "" {
		existingRoot := h.roots[binding.RootID]
		if existingRoot == nil {
			h.mu.Unlock()
			return nil, relayError(ErrRelayTargetUnknown, "unknown relay root %q", binding.RootID)
		}
		chatID = existingRoot.ChatID
	}
	if !h.chatHasLocked(chatID, targetID) {
		h.mu.Unlock()
		return nil, relayError(ErrRelayNotInChat, "%s has not been seen in this chat", targetID)
	}
	if h.mutualRelayLocked(binding.BotID, targetID, chatID, binding.OriginMessageID) {
		h.mu.Unlock()
		return nil, relayError(ErrRelayMutual, "the same message already has an ask from %s to you in this chat", targetID)
	}
	// Every validation that can fail runs before the root is created, so a
	// rejected send never leaves an empty root behind.
	root, err := h.ensureRootLocked(binding)
	if err != nil {
		h.mu.Unlock()
		return nil, err
	}
	if time.Since(root.CreatedAt) > h.opts.RootTTL {
		root.State = relayRootClosed
		h.mu.Unlock()
		h.persist()
		return nil, relayError(ErrRelayBudget, "relay root expired after %s", h.opts.RootTTL)
	}
	if root.EnvelopeCount >= h.opts.MaxEnvelopesPerRoot {
		h.mu.Unlock()
		return nil, relayError(ErrRelayBudget, "relay root reached %d envelopes", h.opts.MaxEnvelopesPerRoot)
	}
	if root.RunCount >= h.opts.MaxRunsPerRoot {
		h.mu.Unlock()
		return nil, relayError(ErrRelayBudget, "relay root reached %d target runs", h.opts.MaxRunsPerRoot)
	}
	kind := RelayKindAsk
	if req.NoReply {
		kind = RelayKindHandoff
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		key = relayIdempotencyKey(binding.BotID, targetID, root.ID, binding.RootID, kind, body)
	}
	if existing := h.envelopeByKeyLocked(binding.BotID, targetID, key); existing != nil {
		copied := *existing
		h.mu.Unlock()
		return &copied, nil
	}
	now := time.Now().UTC()
	envelope := &RelayEnvelope{
		ID:              newRelayID("env"),
		RootID:          root.ID,
		ParentID:        binding.RootID,
		OriginSessionID: root.OriginSessionID,
		Channel:         root.Channel,
		ChatID:          root.ChatID,
		ChatType:        root.ChatType,
		ThreadID:        root.ThreadID,
		From:            binding.BotID,
		To:              targetID,
		Gen:             gen,
		Kind:            kind,
		IdempotencyKey:  key,
		Body:            body,
		State:           RelayStatePending,
		CreatedAt:       now,
	}
	root.EnvelopeCount++
	h.envelopes[envelope.ID] = envelope
	h.mu.Unlock()

	h.persist()

	sender := h.botByName(envelope.From)
	if sender != nil && sender.router != nil {
		if _, err := sender.router.SendRelayMirror(h.ctx, *envelope, target.name); err != nil {
			h.logger.Warn("relay mirror failed", "envelope", envelope.ID, "error", err)
		}
	}
	copied := *envelope
	h.dispatch(envelope)
	return &copied, nil
}

// dispatch delivers an envelope in the background.
func (h *RelayHub) dispatch(envelope *RelayEnvelope) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.deliver(envelope)
	}()
}

func (h *RelayHub) deliver(envelope *RelayEnvelope) {
	h.mu.Lock()
	root := h.roots[envelope.RootID]
	target := h.bots[envelope.To]
	if root == nil || target == nil || target.router == nil {
		envelope.State = RelayStateFailed
		envelope.Error = ErrRelayTargetUnknown
		envelope.UpdatedAt = time.Now().UTC()
		h.mu.Unlock()
		h.persist()
		if envelope.Kind == RelayKindAsk {
			h.sendReply(envelope, "⚠️ Relay failed: "+ErrRelayTargetUnknown)
		}
		return
	}
	if root.State != relayRootOpen {
		envelope.State = RelayStateCanceled
		envelope.Error = "relay root is closed"
		envelope.UpdatedAt = time.Now().UTC()
		h.mu.Unlock()
		h.persist()
		return
	}
	if envelope.Kind != RelayKindReply {
		if root.RunCount >= h.opts.MaxRunsPerRoot {
			envelope.State = RelayStateFailed
			envelope.Error = ErrRelayBudget
			envelope.UpdatedAt = time.Now().UTC()
			h.mu.Unlock()
			h.persist()
			h.sendReply(envelope, "⚠️ Relay stopped: the root reached its run budget.")
			return
		}
	}
	h.noteChatLocked(envelope.ChatID, envelope.To)
	root.RunCount++
	envelope.State = RelayStateRunning
	envelope.UpdatedAt = time.Now().UTC()
	ctx, cancel := context.WithCancel(h.ctx)
	h.runs[envelope.ID] = cancel
	h.mu.Unlock()
	h.persist()

	result, err := target.router.ExecuteRelay(ctx, *envelope)

	h.mu.Lock()
	delete(h.runs, envelope.ID)
	cancel()
	envelope.UpdatedAt = time.Now().UTC()
	if err != nil {
		envelope.State = RelayStateFailed
		envelope.Error = err.Error()
	} else {
		envelope.State = RelayStateDone
	}
	h.mu.Unlock()
	h.persist()

	if envelope.Kind != RelayKindAsk {
		return
	}
	if err != nil {
		h.sendReply(envelope, "⚠️ Relay to "+envelope.To+" failed: "+execguardSafeError(err))
		return
	}
	h.sendReply(envelope, result.Answer)
}

// sendReply delivers a peer answer (or failure notice) back to the origin
// session. Reply envelopes never trigger another automatic reply, which stops
// ping-pong at the source.
func (h *RelayHub) sendReply(source *RelayEnvelope, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		body = "(The peer returned no text.)"
	}
	h.mu.Lock()
	root := h.roots[source.RootID]
	if root == nil || root.State != relayRootOpen {
		h.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	reply := &RelayEnvelope{
		ID:              newRelayID("env"),
		RootID:          root.ID,
		ParentID:        source.ID,
		SessionID:       root.OriginSessionID,
		OriginSessionID: root.OriginSessionID,
		Channel:         root.Channel,
		ChatID:          root.ChatID,
		ChatType:        root.ChatType,
		ThreadID:        root.ThreadID,
		From:            source.To,
		To:              source.From,
		Gen:             source.Gen + 1,
		Kind:            RelayKindReply,
		Body:            body,
		State:           RelayStatePending,
		CreatedAt:       now,
	}
	root.EnvelopeCount++
	h.envelopes[reply.ID] = reply
	h.mu.Unlock()
	h.persist()
	h.dispatch(reply)
}

// CancelRoot closes a root, cancels running envelopes and drops pending ones.
func (h *RelayHub) CancelRoot(rootID string) error {
	rootID = strings.TrimSpace(rootID)
	h.mu.Lock()
	root := h.roots[rootID]
	if root == nil {
		h.mu.Unlock()
		return relayError(ErrRelayTargetUnknown, "unknown relay root %q", rootID)
	}
	root.State = relayRootClosed
	for _, envelope := range h.envelopes {
		if envelope.RootID != rootID {
			continue
		}
		switch envelope.State {
		case RelayStatePending:
			envelope.State = RelayStateCanceled
			envelope.Error = "relay root canceled"
			envelope.UpdatedAt = time.Now().UTC()
		}
	}
	cancels := make([]context.CancelFunc, 0, len(h.runs))
	for envelopeID, cancel := range h.runs {
		if envelope := h.envelopes[envelopeID]; envelope != nil && envelope.RootID == rootID {
			cancels = append(cancels, cancel)
		}
	}
	h.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	h.persist()
	return nil
}

// Roots returns root snapshots, newest first.
func (h *RelayHub) Roots() []*RelayRoot {
	h.mu.Lock()
	defer h.mu.Unlock()
	roots := make([]*RelayRoot, 0, len(h.roots))
	for _, root := range h.roots {
		roots = append(roots, h.rootSnapshotLocked(root))
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].CreatedAt.After(roots[j].CreatedAt) })
	return roots
}

// Root returns one root snapshot, or nil.
func (h *RelayHub) Root(id string) *RelayRoot {
	h.mu.Lock()
	defer h.mu.Unlock()
	root := h.roots[strings.TrimSpace(id)]
	if root == nil {
		return nil
	}
	return h.rootSnapshotLocked(root)
}

// RootsForChat returns root snapshots for one chat.
func (h *RelayHub) RootsForChat(chatID string) []*RelayRoot {
	h.mu.Lock()
	defer h.mu.Unlock()
	roots := make([]*RelayRoot, 0, len(h.roots))
	for _, root := range h.roots {
		if root.ChatID == chatID {
			roots = append(roots, h.rootSnapshotLocked(root))
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].CreatedAt.After(roots[j].CreatedAt) })
	return roots
}

// IsMirrorText reports whether inbound text carries a relay mirror marker.
func (h *RelayHub) IsMirrorText(text string) bool {
	return strings.Contains(text, relayMirrorMarkerPrefix)
}

func (h *RelayHub) rootSnapshotLocked(root *RelayRoot) *RelayRoot {
	copied := *root
	copied.Envelopes = make([]*RelayEnvelope, 0)
	for _, envelope := range h.envelopes {
		if envelope.RootID != root.ID {
			continue
		}
		item := *envelope
		copied.Envelopes = append(copied.Envelopes, &item)
	}
	sort.Slice(copied.Envelopes, func(i, j int) bool {
		return copied.Envelopes[i].CreatedAt.Before(copied.Envelopes[j].CreatedAt)
	})
	return &copied
}

// ensureRootLocked resolves the caller's root, creating it on the first send of
// a human or scheduled run.
func (h *RelayHub) ensureRootLocked(binding *relayTokenBinding) (*RelayRoot, error) {
	if binding.RootID != "" {
		root := h.roots[binding.RootID]
		if root == nil {
			return nil, relayError(ErrRelayTargetUnknown, "unknown relay root %q", binding.RootID)
		}
		return root, nil
	}
	now := time.Now().UTC()
	root := &RelayRoot{
		ID:              newRelayID("root"),
		OriginBot:       binding.BotID,
		OriginSessionID: binding.SessionID,
		OriginRunID:     binding.RunID,
		OriginMessageID: binding.OriginMessageID,
		Channel:         binding.Channel,
		ChatID:          binding.ChatID,
		ChatType:        binding.ChatType,
		ThreadID:        binding.ThreadID,
		OriginPrompt:    binding.Prompt,
		State:           relayRootOpen,
		CreatedAt:       now,
	}
	h.roots[root.ID] = root
	binding.RootID = root.ID
	return root, nil
}

func (h *RelayHub) envelopeByKeyLocked(from, to, key string) *RelayEnvelope {
	if key == "" {
		return nil
	}
	for _, envelope := range h.envelopes {
		if envelope.From == from && envelope.To == to && envelope.IdempotencyKey == key {
			return envelope
		}
	}
	return nil
}

// mutualRelayLocked reports an in-flight ask in the opposite direction that was
// started by the same human message. That pattern means one message reached two
// bots and they are now reviewing each other; asks from different messages are
// independent work and are allowed.
func (h *RelayHub) mutualRelayLocked(from, to, chatID, originMessageID string) bool {
	if strings.TrimSpace(originMessageID) == "" {
		return false
	}
	for _, envelope := range h.envelopes {
		if envelope.Kind != RelayKindAsk || envelope.ChatID != chatID {
			continue
		}
		if envelope.State != RelayStatePending && envelope.State != RelayStateRunning {
			continue
		}
		if envelope.From != to || envelope.To != from {
			continue
		}
		root := h.roots[envelope.RootID]
		if root == nil || root.State != relayRootOpen {
			continue
		}
		if root.OriginMessageID != "" && root.OriginMessageID == originMessageID {
			return true
		}
	}
	return false
}

func (h *RelayHub) chatHasLocked(chatID, botID string) bool {
	members := h.chats[chatID]
	if members == nil {
		return false
	}
	_, ok := members[botID]
	return ok
}

func (h *RelayHub) botByName(id string) *relayBot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bots[id]
}

func (h *RelayHub) persist() {
	h.mu.Lock()
	data, err := marshalRelayState(h.roots, h.envelopes)
	path := h.path
	h.mu.Unlock()
	if err != nil {
		h.logger.Error("encode relay state failed", "error", err)
		return
	}
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		h.logger.Error("create relay state dir failed", "error", err)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "state-*.json")
	if err != nil {
		h.logger.Error("create relay state temp file failed", "error", err)
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		h.logger.Error("write relay state failed", "error", err)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		h.logger.Error("close relay state failed", "error", err)
		return
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		h.logger.Error("replace relay state failed", "error", err)
	}
}

func (h *RelayHub) load() error {
	data, err := os.ReadFile(h.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read relay state: %w", err)
	}
	var state relayStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode relay state %s: %w", h.path, err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, envelope := range state.Envelopes {
		if envelope != nil && envelope.ID != "" {
			h.envelopes[envelope.ID] = envelope
		}
	}
	counts := make(map[string]int, len(h.envelopes))
	for _, envelope := range h.envelopes {
		counts[envelope.RootID]++
	}
	for _, root := range state.Roots {
		if root == nil || root.ID == "" {
			continue
		}
		root.Envelopes = nil
		root.EnvelopeCount = counts[root.ID]
		// Earlier builds could journal a root before a rejected send; drop
		// roots that never produced an envelope.
		if root.EnvelopeCount == 0 {
			continue
		}
		h.roots[root.ID] = root
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func truncateRelayPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	const limit = 4000
	if len(prompt) > limit {
		return prompt[:limit] + "…"
	}
	return prompt
}

// execguardSafeError trims error text that is echoed back into a chat.
func execguardSafeError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	const limit = 2000
	if len(text) > limit {
		return text[:limit] + "…"
	}
	return text
}
