package core

import "context"

// ScheduledTask is reserved for v1.1. Triggers reuse the normal message path.
type ScheduledTask struct {
	ID       string
	CronExpr string
	Key      SessionKey
	Prompt   string
	Enabled  bool
}

type Scheduler interface {
	Add(ctx context.Context, task ScheduledTask) error
	Remove(ctx context.Context, id string) error
	List(ctx context.Context) ([]ScheduledTask, error)
	Start(ctx context.Context, trigger func(ScheduledTask)) error
}
