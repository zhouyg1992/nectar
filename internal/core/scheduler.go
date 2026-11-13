package core

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

const (
	schedulerMaxSleep = 30 * time.Second
)

type ScheduleTrigger func(task ScheduledTask, anchor SessionAnchor) error

type SchedulerOptions struct {
	StorePath       string
	Anchors         *AnchorStore
	DefaultTimezone string
	StartupGrace    time.Duration
	HistoryLimit    int
	Logger          *slog.Logger
	Now             func() time.Time
}

// Scheduler owns all task state in memory and persists a full snapshot after
// every mutation. The loop wakes at least every 30s so wall-clock changes
// (sleep/resume, manual clock changes) cannot strand a timer.
type Scheduler struct {
	mu              sync.RWMutex
	tasks           map[string]*ScheduledTask
	running         map[string]bool
	anchors         *AnchorStore
	trigger         ScheduleTrigger
	storePath       string
	defaultTimezone string
	startupGrace    time.Duration
	historyLimit    int
	logger          *slog.Logger
	now             func() time.Time

	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	started bool
	stopped bool
}

func NewScheduler(opts SchedulerOptions) (*Scheduler, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	s := &Scheduler{
		tasks:           make(map[string]*ScheduledTask),
		running:         make(map[string]bool),
		anchors:         opts.Anchors,
		storePath:       opts.StorePath,
		defaultTimezone: opts.DefaultTimezone,
		startupGrace:    opts.StartupGrace,
		historyLimit:    opts.HistoryLimit,
		logger:          logger,
		now:             now,
		wake:            make(chan struct{}, 1),
	}
	if opts.StorePath != "" {
		tasks, err := loadScheduleTasks(opts.StorePath)
		if err != nil {
			return nil, err
		}
		for i := range tasks {
			task := tasks[i]
			s.tasks[task.ID] = &task
		}
	}
	return s, nil
}

func (s *Scheduler) Start(ctx context.Context, trigger ScheduleTrigger) error {
	if trigger == nil {
		return fmt.Errorf("scheduler trigger is required")
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	if s.stopped {
		s.mu.Unlock()
		return fmt.Errorf("scheduler already stopped")
	}
	s.started = true
	s.trigger = trigger
	s.stop = make(chan struct{}, 1)
	s.done = make(chan struct{})
	s.recoverLocked(s.now())
	if err := s.saveLocked(); err != nil {
		s.logger.Warn("could not persist recovered schedule state", "error", err)
	}
	s.mu.Unlock()

	go s.loop(ctx)
	return nil
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	stopCh := s.stop
	doneCh := s.done
	s.mu.Unlock()

	select {
	case stopCh <- struct{}{}:
	default:
	}
	<-doneCh
}

func (s *Scheduler) recoverLocked(now time.Time) {
	for _, task := range s.tasks {
		if task.Status != ScheduleActive {
			task.NextRun = time.Time{}
			continue
		}
		switch task.Mode {
		case ScheduleOnce:
			if task.ExecuteAt.IsZero() {
				task.Status = ScheduleFailed
				task.LastError = "one-time task has no execute_at"
				continue
			}
			if task.ExecuteAt.After(now) {
				task.NextRun = task.ExecuteAt
				continue
			}
			if now.Sub(task.ExecuteAt) <= s.startupGrace {
				task.NextRun = now
				continue
			}
			task.Status = ScheduleCompleted
			task.LastError = fmt.Sprintf("missed by %s", now.Sub(task.ExecuteAt).Round(time.Second))
			task.NextRun = time.Time{}
		case ScheduleCron, ScheduleEvery:
			if !task.NextRun.IsZero() && task.NextRun.Before(now) && now.Sub(task.NextRun) <= s.startupGrace {
				task.NextRun = now
				continue
			}
			next, err := nextRunFor(task, now)
			if err != nil {
				task.Status = ScheduleFailed
				task.LastError = err.Error()
				task.NextRun = time.Time{}
				continue
			}
			task.NextRun = next
		}
	}
}

func (s *Scheduler) loop(ctx context.Context) {
	defer close(s.done)
	for {
		wait := s.nextWait()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.stop:
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
			s.fireDue(s.now())
		}
	}
}

func (s *Scheduler) nextWait() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var earliest time.Time
	for _, task := range s.tasks {
		if task.Status != ScheduleActive || task.NextRun.IsZero() {
			continue
		}
		if earliest.IsZero() || task.NextRun.Before(earliest) {
			earliest = task.NextRun
		}
	}
	if earliest.IsZero() {
		return time.Hour
	}
	wait := earliest.Sub(s.now())
	if wait < 0 {
		wait = 0
	}
	if wait > schedulerMaxSleep {
		wait = schedulerMaxSleep
	}
	return wait
}

func (s *Scheduler) signalWake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) fireDue(now time.Time) {
	s.mu.Lock()
	var due []ScheduledTask
	for _, task := range s.tasks {
		if task.Status != ScheduleActive || task.NextRun.IsZero() || task.NextRun.After(now) {
			continue
		}
		if s.running[task.ID] {
			next, err := nextRunFor(task, now)
			if err != nil {
				task.Status = ScheduleFailed
				task.NextRun = time.Time{}
				task.LastError = err.Error()
			} else {
				task.NextRun = next
				task.LastError = "previous run still in progress; occurrence skipped"
			}
			task.UpdatedAt = now
			continue
		}
		copy := *task
		s.running[task.ID] = true
		if task.Mode == ScheduleOnce {
			task.NextRun = time.Time{}
		} else {
			next, err := nextRunFor(task, now)
			if err != nil {
				task.LastError = err.Error()
				task.Status = ScheduleFailed
				task.NextRun = time.Time{}
			} else {
				task.NextRun = next
			}
		}
		task.UpdatedAt = now
		due = append(due, copy)
	}
	if err := s.saveLocked(); err != nil {
		s.logger.Error("could not persist due schedule state", "error", err)
	}
	s.mu.Unlock()

	for _, task := range due {
		go s.execute(task, true)
	}
}

func (s *Scheduler) execute(task ScheduledTask, consumeOccurrence bool) {
	startedAt := s.now()
	var anchor SessionAnchor
	var runErr error
	if s.anchors != nil {
		anchor, _ = s.anchors.Get(task.Key)
	}
	if anchor.ChatID == "" {
		runErr = fmt.Errorf("no delivery target for chat %s; send a message in that chat first", task.Key.ChatID)
	} else if s.trigger != nil {
		runErr = s.trigger(task, anchor)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, task.ID)
	current, ok := s.tasks[task.ID]
	if !ok {
		s.signalWake()
		return
	}
	current.LastRun = startedAt
	current.RunCount++
	current.UpdatedAt = s.now()
	if runErr != nil {
		current.LastError = runErr.Error()
		s.logger.Warn("scheduled task failed", "id", task.ID, "error", runErr)
	} else {
		current.LastError = ""
	}
	if consumeOccurrence && current.Mode == ScheduleOnce {
		current.NextRun = time.Time{}
		if runErr != nil {
			current.Status = ScheduleFailed
		} else {
			current.Status = ScheduleCompleted
		}
	}
	if err := s.saveLocked(); err != nil {
		s.logger.Error("could not persist completed schedule run", "id", task.ID, "error", err)
	}
	s.signalWake()
}

func (s *Scheduler) Add(req ScheduleRequest, createdBy string) (*ScheduledTask, error) {
	task, err := buildScheduledTask(req, s.now(), s.defaultTimezone, createdBy)
	if err != nil {
		return nil, err
	}
	if s.anchors != nil {
		if _, ok := s.anchors.Get(task.Key); !ok {
			return nil, fmt.Errorf("no delivery target for chat %s; send a message in that chat first", task.Key.ChatID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[task.ID]; exists {
		return nil, fmt.Errorf("schedule %s already exists", task.ID)
	}
	s.tasks[task.ID] = task
	if err := s.saveLocked(); err != nil {
		delete(s.tasks, task.ID)
		return nil, err
	}
	copy := *task
	s.signalWake()
	return &copy, nil
}

func (s *Scheduler) Preview(req ScheduleRequest) ([]time.Time, error) {
	if req.ChatID == "" {
		req.ChatID = "preview"
	}
	if req.Channel == "" {
		req.Channel = "preview"
	}
	if req.Prompt == "" {
		req.Prompt = "preview"
	}
	now := s.now()
	task, err := buildScheduledTask(req, now, s.defaultTimezone, "")
	if err != nil {
		return nil, err
	}
	switch task.Mode {
	case ScheduleOnce:
		return []time.Time{task.ExecuteAt}, nil
	case ScheduleCron:
		spec, err := parseCronExpression(task.CronExpr)
		if err != nil {
			return nil, err
		}
		if spec.loc == nil && task.Timezone != "" {
			loc, err := scheduleLocation(task.Timezone)
			if err != nil {
				return nil, err
			}
			spec.loc = loc
		}
		return spec.nextN(now, 3)
	case ScheduleEvery:
		every, err := parseEvery(task.Every)
		if err != nil {
			return nil, err
		}
		out := make([]time.Time, 0, 3)
		cursor := now
		for len(out) < 3 {
			next := nextEvery(task.AnchorAt, every, cursor)
			out = append(out, next)
			cursor = next
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown schedule mode")
	}
}

func (s *Scheduler) List(includeAll bool) []ScheduledTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ScheduledTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		if !includeAll && task.Status != ScheduleActive && task.Status != SchedulePaused {
			continue
		}
		out = append(out, *task)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Scheduler) Get(id string) (ScheduledTask, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	if !ok {
		return ScheduledTask{}, false
	}
	return *task, true
}

func (s *Scheduler) Pause(id string) error {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("schedule %s not found", id)
	}
	if task.Status == SchedulePaused {
		return nil
	}
	if task.Status != ScheduleActive {
		return fmt.Errorf("schedule %s is %s, not active", id, task.Status)
	}
	if s.running[id] && task.Mode == ScheduleOnce {
		return fmt.Errorf("schedule %s is currently running", id)
	}
	previous := *task
	if task.Mode == ScheduleOnce {
		remaining := task.ExecuteAt.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		task.RemainingSeconds = int64(remaining / time.Second)
		task.ExecuteAt = time.Time{}
	}
	task.Status = SchedulePaused
	task.NextRun = time.Time{}
	task.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		*task = previous
		return err
	}
	s.signalWake()
	return nil
}

func (s *Scheduler) Resume(id string) error {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("schedule %s not found", id)
	}
	if task.Status == ScheduleActive {
		return nil
	}
	if task.Status != SchedulePaused {
		return fmt.Errorf("schedule %s is %s, not paused", id, task.Status)
	}
	previous := *task
	task.Status = ScheduleActive
	if task.Mode == ScheduleOnce {
		if task.ExecuteAt.IsZero() {
			task.ExecuteAt = now.Add(time.Duration(task.RemainingSeconds) * time.Second)
			task.RemainingSeconds = 0
		}
		task.NextRun = task.ExecuteAt
	} else {
		next, err := nextRunFor(task, now)
		if err != nil {
			*task = previous
			return err
		}
		task.NextRun = next
	}
	task.UpdatedAt = now
	if err := s.saveLocked(); err != nil {
		*task = previous
		return err
	}
	s.signalWake()
	return nil
}

func (s *Scheduler) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("schedule %s not found", id)
	}
	delete(s.tasks, id)
	if err := s.saveLocked(); err != nil {
		s.tasks[id] = task
		return err
	}
	s.signalWake()
	return nil
}

func (s *Scheduler) Update(id string, patch SchedulePatch) (*ScheduledTask, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("schedule %s not found", id)
	}
	if s.running[id] {
		return nil, fmt.Errorf("schedule %s is currently running", id)
	}
	previous := *task
	if err := applySchedulePatch(task, patch, now); err != nil {
		*task = previous
		return nil, err
	}
	if err := s.saveLocked(); err != nil {
		*task = previous
		return nil, err
	}
	copy := *task
	s.signalWake()
	return &copy, nil
}

func (s *Scheduler) RunNow(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("schedule %s not found", id)
	}
	if s.running[id] {
		return fmt.Errorf("schedule %s is already running", id)
	}
	if task.Status == ScheduleCompleted || task.Status == ScheduleFailed {
		return fmt.Errorf("schedule %s is %s", id, task.Status)
	}
	s.running[id] = true
	copy := *task
	go s.execute(copy, false)
	return nil
}

func (s *Scheduler) saveLocked() error {
	pruned := s.pruneHistoryLocked()
	if s.storePath == "" {
		return nil
	}
	tasks := make([]ScheduledTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, *task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	if err := saveScheduleTasks(s.storePath, tasks); err != nil {
		for id, task := range pruned {
			s.tasks[id] = task
		}
		return err
	}
	return nil
}

func (s *Scheduler) pruneHistoryLocked() map[string]*ScheduledTask {
	if s.historyLimit <= 0 {
		return nil
	}
	var finished []*ScheduledTask
	for _, task := range s.tasks {
		if task.Status == ScheduleCompleted || task.Status == ScheduleFailed {
			finished = append(finished, task)
		}
	}
	if len(finished) <= s.historyLimit {
		return nil
	}
	sort.Slice(finished, func(i, j int) bool {
		if finished[i].UpdatedAt.Equal(finished[j].UpdatedAt) {
			return finished[i].ID < finished[j].ID
		}
		return finished[i].UpdatedAt.Before(finished[j].UpdatedAt)
	})
	pruned := make(map[string]*ScheduledTask, len(finished)-s.historyLimit)
	for _, task := range finished[:len(finished)-s.historyLimit] {
		pruned[task.ID] = task
		delete(s.tasks, task.ID)
	}
	return pruned
}
