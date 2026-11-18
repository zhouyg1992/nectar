package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Relay echo levels mirror config.RelayEcho*. The config package validates the
// value and the hub passes it through; core compares against these constants.
const (
	RelayEchoFull    = "full"
	RelayEchoSummary = "summary"
	RelayEchoOff     = "off"
)

// Relay envelope kinds. ask waits for the target's final answer and delivers
// it back to the sender's origin session; handoff is fire-and-forget; reply is
// the hub-created answer envelope and never triggers another automatic reply.
const (
	RelayKindAsk     = "ask"
	RelayKindReply   = "reply"
	RelayKindHandoff = "handoff"
)

// Relay envelope and root states.
const (
	RelayStatePending  = "pending"
	RelayStateRunning  = "running"
	RelayStateDone     = "done"
	RelayStateFailed   = "failed"
	RelayStateCanceled = "canceled"

	relayRootOpen   = "open"
	relayRootClosed = "closed"
)

// Relay error codes are stable machine-readable strings; the CLI prints them
// verbatim so agents can stop instead of retrying.
const (
	ErrRelayDisabled      = "ERR_DISABLED"
	ErrRelayGenLimit      = "ERR_GEN_LIMIT"
	ErrRelayBudget        = "ERR_BUDGET"
	ErrRelayTargetUnknown = "ERR_TARGET_UNKNOWN"
	ErrRelayRejected      = "ERR_REJECTED"
	ErrRelayUnauthorized  = "ERR_UNAUTHORIZED"
	ErrRelayMutual        = "ERR_MUTUAL_RELAY"
	ErrRelayDirectChat    = "ERR_DIRECT_CHAT"
)

// RelayError carries a stable code so the API and CLI can expose it without
// parsing prose.
type RelayError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RelayError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func relayError(code, format string, args ...any) error {
	return &RelayError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// RelayErrorCode returns the stable code for a relay error, or an empty string.
func RelayErrorCode(err error) string {
	var relayErr *RelayError
	if errors.As(err, &relayErr) {
		return relayErr.Code
	}
	return ""
}

// RelayEnvelope is one relay message. SessionID is where the envelope runs
// (empty for ask: the target's per-root session). OriginSessionID is where an
// automatic reply must land.
type RelayEnvelope struct {
	ID              string    `json:"id"`
	RootID          string    `json:"root_id"`
	ParentID        string    `json:"parent_id,omitempty"`
	SessionID       string    `json:"session_id,omitempty"`
	OriginSessionID string    `json:"origin_session_id,omitempty"`
	Channel         string    `json:"channel"`
	ChatID          string    `json:"chat_id"`
	ChatType        string    `json:"chat_type,omitempty"`
	ThreadID        string    `json:"thread_id,omitempty"`
	From            string    `json:"from"`
	To              string    `json:"to"`
	Gen             int       `json:"gen"`
	Kind            string    `json:"kind"`
	IdempotencyKey  string    `json:"idempotency_key,omitempty"`
	Body            string    `json:"body"`
	State           string    `json:"state"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

// RelayRoot is a human-initiated collaboration tree. It is created lazily on
// the first relay send from a run rather than on every inbound message.
type RelayRoot struct {
	ID              string           `json:"id"`
	OriginBot       string           `json:"origin_bot"`
	OriginSessionID string           `json:"origin_session_id"`
	OriginRunID     string           `json:"origin_run_id,omitempty"`
	OriginMessageID string           `json:"origin_message_id,omitempty"`
	OriginUserID    string           `json:"origin_user_id,omitempty"`
	Channel         string           `json:"channel"`
	ChatID          string           `json:"chat_id"`
	ChatType        string           `json:"chat_type,omitempty"`
	ThreadID        string           `json:"thread_id,omitempty"`
	OriginPrompt    string           `json:"-"`
	State           string           `json:"state"`
	CreatedAt       time.Time        `json:"created_at"`
	EnvelopeCount   int              `json:"envelope_count"`
	RunCount        int              `json:"run_count"`
	Envelopes       []*RelayEnvelope `json:"envelopes,omitempty"`
}

// RelayPeer is the roster entry an agent or operator sees.
type RelayPeer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role,omitempty"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	AcceptFrom   []string `json:"accept_from,omitempty"`
	AllowedChats []string `json:"allowed_chats,omitempty"`
	Busy         bool     `json:"busy"`
}

// RelaySendRequest is the validated shape of POST /relay/send. Identity fields
// are derived from the run token, never from this body.
type RelaySendRequest struct {
	To             string `json:"to"`
	Body           string `json:"body"`
	NoReply        bool   `json:"no_reply"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// RelayTokenRequest is filled in by the router when a run starts.
type RelayTokenRequest struct {
	BotID           string
	SessionID       string
	RunID           string
	RootID          string
	Gen             int
	Prompt          string
	Channel         string
	ChatID          string
	ChatType        string
	ThreadID        string
	OriginMessageID string
	OriginUserID    string
}

type relayTokenBinding struct {
	RelayTokenRequest
	Token     string
	CreatedAt time.Time
}

type relayBot struct {
	id           string
	name         string
	role         string
	description  string
	capabilities []string
	acceptFrom   []string
	allowedChats []string
	router       *Router
}

type relayStateFile struct {
	Version   int              `json:"version"`
	Roots     []*RelayRoot     `json:"roots"`
	Envelopes []*RelayEnvelope `json:"envelopes"`
}

const relayStateVersion = 1

// relayMirrorMarkerPrefix makes mirror text recognizable on inbound even when
// the platform does not report sender type. It is a pure zero-width sequence
// (INVISIBLE SEPARATOR, ZERO WIDTH SPACE, INVISIBLE SEPARATOR) so humans never
// see plumbing in the chat.
const relayMirrorMarkerPrefix = "\u2063\u200b\u2063"

// relayMirrorTitle is the visible hand-off card title. It doubles as an inbound
// fallback for platforms that strip zero-width characters.
const relayMirrorTitle = "🔁 Relay handoff"

func relayMirrorMarker() string {
	return relayMirrorMarkerPrefix
}

func newRelayID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf[:])
}

// relaySessionName is the detached session name for a root's target runs.
func relaySessionName(rootID string) string {
	return "relay:" + shortRelayID(rootID)
}

func relayRecoverySessionName(rootID string) string {
	return "relay-recovery:" + shortRelayID(rootID)
}

func shortRelayID(id string) string {
	id = strings.TrimSpace(id)
	if index := strings.LastIndex(id, "_"); index >= 0 && index+1 < len(id) {
		id = id[index+1:]
	}
	if len(id) > 8 {
		id = id[:8]
	}
	if id == "" {
		id = "root"
	}
	return id
}

// relayIdempotencyKey hashes everything that makes a send distinct, so a
// legitimate repeat of the same short body is not collapsed by accident.
func relayIdempotencyKey(from, to, rootID, parentID, kind, body string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{from, to, rootID, parentID, kind, body}, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// MirrorSummary keeps the visible handoff card to one short line.
func mirrorSummary(body string, limit int) string {
	line := strings.TrimSpace(body)
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	line = strings.Join(strings.Fields(line), " ")
	if limit > 0 && len([]rune(line)) > limit {
		runes := []rune(line)
		line = string(runes[:limit]) + "…"
	}
	return line
}

func marshalRelayState(roots map[string]*RelayRoot, envelopes map[string]*RelayEnvelope) ([]byte, error) {
	state := relayStateFile{Version: relayStateVersion}
	for _, root := range roots {
		copied := *root
		copied.Envelopes = nil
		state.Roots = append(state.Roots, &copied)
	}
	for _, envelope := range envelopes {
		copied := *envelope
		state.Envelopes = append(state.Envelopes, &copied)
	}
	sortRelayRoots(state.Roots)
	return json.MarshalIndent(state, "", "  ")
}

func sortRelayRoots(roots []*RelayRoot) {
	for i := 1; i < len(roots); i++ {
		for j := i; j > 0 && roots[j].CreatedAt.Before(roots[j-1].CreatedAt); j-- {
			roots[j], roots[j-1] = roots[j-1], roots[j]
		}
	}
}
