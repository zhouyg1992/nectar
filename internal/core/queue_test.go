package core

import (
	"sync"
	"testing"
	"time"
)

func TestQueueSerializesSameKey(t *testing.T) {
	q := NewQueue()
	var mu sync.Mutex
	var order []int
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		i := i
		q.Do("same", func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("queue jobs did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	for i, got := range order {
		if got != i {
			t.Fatalf("execution order = %v, want 0..4", order)
		}
	}
}

func TestQueueAllowsDifferentKeys(t *testing.T) {
	q := NewQueue()
	started := make(chan struct{}, 2)
	release := make(chan struct{})

	q.Do("a", func() {
		started <- struct{}{}
		<-release
	})
	q.Do("b", func() {
		started <- struct{}{}
		<-release
	})

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("different keys were serialized")
		}
	}
	close(release)
}
