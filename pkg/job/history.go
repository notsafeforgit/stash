package job

import "context"

// History stores admission and terminal outcomes for ordinary library tasks.
// Non-checkpointed executors are interrupted on restart. Explicitly persisted
// commands can be restored at startup; arbitrary executors are never replayed.
// Archive workers retain their separate lease/checkpoint protocol in durable.go.
type History interface {
	Create(context.Context, Job) error
	Save(context.Context, Job) error
	Find(context.Context, string) (*Job, error)
	List(context.Context, int, int) ([]Job, error)
	Interrupt(context.Context) error
	Cancel(context.Context, string, int) error
}
