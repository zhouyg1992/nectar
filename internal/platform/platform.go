package platform

import "context"

type CardStatus string

const (
	CardThinking CardStatus = "thinking"
	CardInfo     CardStatus = "info"
	CardComplete CardStatus = "complete"
	CardError    CardStatus = "error"
)

// Card is a platform-neutral status card rendered by each adapter.
type Card struct {
	Title  string
	Body   string
	Status CardStatus
	Footer string
}

// Message is the normalized form of an inbound chat message.
type Message struct {
	Channel   string
	MessageID string
	ChatID    string
	ChatType  string
	UserID    string
	Text      string
	MediaKeys []string
	ReplyTo   string
	ThreadID  string
}

// Reply is a message to send back to the chat platform.
type Reply struct {
	ChatID        string
	ReplyTo       string
	Text          string
	Card          *Card
	Media         []Media
	ThreadID      string
	ReplyInThread bool
}

// Media is a local file to upload back to the chat platform.
type Media struct {
	Type string
	Path string
}

// Capabilities describes the optional features a platform supports.
type Capabilities struct {
	Streaming   bool
	Card        bool
	Media       bool
	Group       bool
	MentionOnly bool
}

// PlatformAdapter is implemented once per chat platform.
type PlatformAdapter interface {
	Name() string
	Capabilities() Capabilities

	// Start blocks until ctx is canceled or the connection terminates.
	// handler must return quickly; long-running work belongs in a queue.
	Start(ctx context.Context, handler func(Message)) error

	Send(ctx context.Context, reply Reply) (messageID string, err error)
	Update(ctx context.Context, messageID string, reply Reply) error
	Download(ctx context.Context, mediaKey string) (localPath string, err error)
}
