package execguard

import "sync"

// WorkspaceLocks is a process-wide lock registry. Multiple bot runtimes must
// share one instance so two bots cannot write the same workspace lane at once.
type WorkspaceLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewWorkspaceLocks() *WorkspaceLocks {
	return &WorkspaceLocks{locks: make(map[string]*sync.Mutex)}
}

func (l *WorkspaceLocks) Lock(key string) func() {
	if l == nil {
		return func() {}
	}
	l.mu.Lock()
	lock := l.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		l.locks[key] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}
