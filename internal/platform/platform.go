package platform

import (
	"context"

	"github.com/zhouyg1992/nectar/internal/attachment"
)

type CardStatus string

const (
	CardThinking CardStatus = "thinking"
	CardInfo     CardStatus = "info"
	CardComplete CardStatus = "complete"
	// CardFinalizing is a visually complete, still-updatable state used while
	// usage metadata is collected after the answer is ready.
	CardFinalizing CardStatus = "finalizing"
	CardError      CardStatus = "error"
)

// Card is a platform-neutral status card rendered by each adapter.
type Card struct {
	Title    string
	Activity string
	Body     string
	Status   CardStatus
	Footer   string
}

// Message is the normalized form of an inbound chat message.
type Message struct {
	Channel   string
	MessageID string
	ChatID    string
	ChatType  string
	UserID    string
	Text      string
	// Origin is empty for inbound chat messages, "schedule" for tasks fired
	// by the scheduler, and "relay" for prompts delivered by the multi-bot
	// relay bus. Non-empty origins skip slash-command parsing.
	Origin string
	// FromBot reports that the platform attributes the message to another
	// bot. Nectar drops such messages so a relay mirror cannot trigger the
	// bot it was meant to be visible to.
	FromBot     bool
	Attachments []attachment.Resource
	ReplyTo     string
	ThreadID    string
	// ReplyContext carries platform-private reply state (for example a
	// DingTalk sessionWebhook). Callers must treat it as opaque.
	ReplyContext any
}

// Reply is a message to send back to the chat platform.
type Reply struct {
	ChatID        string
	ChatType      string
	UserID        string
	ReplyTo       string
	Text          string
	Card          *Card
	Media         []Media
	ThreadID      string
	ReplyInThread bool
	// ReplyContext is copied from the inbound Message by the router.
	ReplyContext any
}

// Media is a local file to upload back to the chat platform.
type Media struct {
	Type     string
	Path     string
	Name     string
	MimeType string
}

// Capabilities describes the optional features a platform supports.
type Capabilities struct {
	Streaming   bool
	Card        bool
	Media       bool
	Group       bool
	MentionOnly bool
}

// TypingIndicator is an optional platform extension for showing lightweight
// progress feedback while an agent turn is running. The returned stop function
// must be safe to call once.
type TypingIndicator interface {
	StartTyping(ctx context.Context, replyContext any) (stop func())
}

// PlatformAdapter is implemented once per chat platform.
type PlatformAdapter interface {
	Name() string
	Capabilities() Capabilities

	// Start blocks until ctx is canceled or the connection terminates.
	// handler must return quickly; long-running work belongs in a queue.
	Start(ctx context.Context, handler func(Message)) error

	Send(ctx context.Context, reply Reply) (messageID string, err error)
	// Update edits a previously sent message. reply carries the destination
	// chat and thread so adapters that need them (for example Telegram) can
	// address the edit.
	Update(ctx context.Context, messageID string, reply Reply) error
	// Download writes an inbound resource to dir and returns its local path.
	// The adapter owns platform-specific naming and MIME detection.
	Download(ctx context.Context, resource attachment.Resource, dir string) (attachment.Local, error)
}
