package execguard

import (
	"bytes"
	"sync"
)

// LimitedBuffer keeps at most max bytes and silently drops the rest.
type LimitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	max       int
	truncated bool
}

func NewLimitedBuffer(max int) *LimitedBuffer {
	return &LimitedBuffer{max: max}
}

func (b *LimitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max > 0 && b.buf.Len() >= b.max {
		b.truncated = true
		return len(p), nil
	}
	remaining := len(p)
	if b.max > 0 && remaining > b.max-b.buf.Len() {
		remaining = b.max - b.buf.Len()
		b.truncated = true
	}
	if _, err := b.buf.Write(p[:remaining]); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (b *LimitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *LimitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// Truncate shortens s to max bytes, preserving valid UTF-8 boundaries.
func Truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	for max > 0 && s[max]&0xC0 == 0x80 {
		max--
	}
	return s[:max]
}
