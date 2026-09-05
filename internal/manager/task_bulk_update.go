package manager

import (
	"context"
	"fmt"
	"strings"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

type BulkUpdateOperation interface {
	Update(ctx context.Context, id int) error
}

type bulkUpdateJob struct {
	repo         models.Repository
	hookExecutor HookExecutor
	ids          []int
	operation    BulkUpdateOperation
	hookType     hook.TriggerEnum
	inputFields  []string
	input        interface{}
	description  string
}

func (j *bulkUpdateJob) Execute(ctx context.Context, progress *job.Progress) error {
	total := len(j.ids)
	progress.SetTotal(total)

	runHooks := config.GetBulkUpdateHooks()

	// Each operation can write several fields and relationships. A failed
	// item must roll back in full, independently of the post-hook setting.
	failed := 0
	var failures []string
	for i, id := range j.ids {
		if job.IsCancelled(ctx) {
			break
		}

		err := j.repo.WithTxn(ctx, func(ctx context.Context) error {
			return j.operation.Update(ctx, id)
		})

		if err != nil {
			failed++
			logger.Errorf("error updating item %d: %v", id, err)
			// Bound the job's retained error message; the log contains every failure.
			if len(failures) < 20 {
				failures = append(failures, fmt.Sprintf("%d: %v", id, err))
			}
		} else if runHooks && j.hookExecutor != nil {
			j.hookExecutor.ExecutePostHooks(ctx, id, j.hookType, j.input, j.inputFields)
		}

		progress.SetProcessed(i + 1)
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d items failed: %s", failed, total, strings.Join(failures, "; "))
	}
	return nil
}

func (s *Manager) BulkUpdate(ctx context.Context, description string, ids []int, operation BulkUpdateOperation, hookType hook.TriggerEnum, input interface{}, inputFields []string) int {
	j := &bulkUpdateJob{
		repo:         s.Repository,
		hookExecutor: s.HookExecutor,
		ids:          ids,
		operation:    operation,
		hookType:     hookType,
		input:        input,
		inputFields:  inputFields,
		description:  description,
	}

	return s.JobManager.Add(ctx, description, j)
}
