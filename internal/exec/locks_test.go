package execguard

import (
	"testing"
	"time"
)

func TestWorkspaceLocksSerializeSameKey(t *testing.T) {
	locks := NewWorkspaceLocks()
	unlockFirst := locks.Lock("workspace\x00chat")
	acquired := make(chan struct{})
	go func() {
		unlockSecond := locks.Lock("workspace\x00chat")
		close(acquired)
		unlockSecond()
	}()
	select {
	case <-acquired:
		t.Fatal("second lock acquired before first released")
	case <-time.After(50 * time.Millisecond):
	}
	unlockFirst()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second lock was not released")
	}
}
