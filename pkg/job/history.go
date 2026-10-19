package job

import "context"

// History stores admission and terminal outcomes for ordinary library tasks.
// Their executors are not serializable/checkpointed; an interrupted task must
// never be rerun automatically. Durable archive workers retain their separate
// fenced lease/checkpoint protocol in durable.go.
type History interface {
	Create(context.Context, Job) error
	Save(context.Context, Job) error
	Find(context.Context, string) (*Job, error)
	List(context.Context, int, int) ([]Job, error)
	Interrupt(context.Context) error
}
