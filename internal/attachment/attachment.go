// Package attachment defines platform- and engine-neutral inbound attachments.
package attachment

// Kind identifies the kind of content carried by an attachment.
type Kind string

const (
	KindImage Kind = "image"
	KindFile  Kind = "file"
	KindAudio Kind = "audio"
	KindVideo Kind = "video"
)

// Resource is an opaque platform attachment that can be downloaded later.
// Key is only meaningful to the platform adapter that produced it.
type Resource struct {
	Kind      Kind
	Key       string
	Name      string
	MessageID string
}

// Local is a downloaded attachment available to an engine.
type Local struct {
	Kind     Kind
	Path     string
	Name     string
	MimeType string
}
