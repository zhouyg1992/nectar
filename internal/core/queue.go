package core

import "sync"

// Queue serializes jobs that share a key. Different keys run concurrently.
type Queue struct {
	mu     sync.Mutex
	chains map[string]*chain
}

type chain struct {
	jobs    []func()
	running bool
}

func NewQueue() *Queue {
	return &Queue{chains: make(map[string]*chain)}
}

// Do runs fn asynchronously. Jobs with the same key run in submission order.
func (q *Queue) Do(key string, fn func()) {
	q.mu.Lock()
	c := q.chains[key]
	if c == nil {
		c = &chain{}
		q.chains[key] = c
	}
	c.jobs = append(c.jobs, fn)
	if !c.running {
		c.running = true
		go q.run(key, c)
	}
	q.mu.Unlock()
}

func (q *Queue) run(key string, c *chain) {
	for {
		q.mu.Lock()
		if len(c.jobs) == 0 {
			c.running = false
			if q.chains[key] == c {
				delete(q.chains, key)
			}
			q.mu.Unlock()
			return
		}
		job := c.jobs[0]
		c.jobs[0] = nil
		c.jobs = c.jobs[1:]
		q.mu.Unlock()

		job()
	}
}
