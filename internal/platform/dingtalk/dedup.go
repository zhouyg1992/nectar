package dingtalk

import (
	"sync"
	"time"
)

type messageDedup struct {
	mu   sync.Mutex
	seen map[string]time.Time
	ttl  time.Duration
}

func newMessageDedup(ttl time.Duration) *messageDedup {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &messageDedup{seen: make(map[string]time.Time), ttl: ttl}
}

func (d *messageDedup) duplicate(id string) bool {
	if id == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for key, at := range d.seen {
		if now.Sub(at) > d.ttl {
			delete(d.seen, key)
		}
	}
	if _, ok := d.seen[id]; ok {
		return true
	}
	d.seen[id] = now
	return false
}
