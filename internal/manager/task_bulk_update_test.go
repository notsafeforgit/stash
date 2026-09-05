package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/assert"
)

type testHookExecutor struct {
	calls []int
}

func (t *testHookExecutor) ExecutePostHooks(ctx context.Context, id int, hookType hook.TriggerEnum, input interface{}, inputFields []string) {
	t.calls = append(t.calls, id)
}

type testBulkUpdateOperation struct {
	updated *[]int
}

func (o testBulkUpdateOperation) Update(ctx context.Context, id int) error {
	*o.updated = append(*o.updated, id)
	return nil
}

type testBulkUpdateOperationWithFailures struct {
	attempted *[]int
	failures  map[int]error
}

func (o testBulkUpdateOperationWithFailures) Update(ctx context.Context, id int) error {
	*o.attempted = append(*o.attempted, id)
	if err, ok := o.failures[id]; ok {
		return err
	}

	return nil
}

func TestBulkUpdateJobRunsHooksWhenEnabled(t *testing.T) {
	config.InitializeEmpty()
	config.GetInstance().SetBool(config.BulkUpdateHooks, true)

	db := mocks.NewDatabase()
	jobMgr := job.NewManager()
	jobMgr.SetSync(true)

	hooks := &testHookExecutor{}
	mgr := &Manager{
		Repository:   db.Repository(),
		JobManager:   jobMgr,
		HookExecutor: hooks,
	}

	var updated []int
	jobID := mgr.BulkUpdate(context.Background(), "Bulk test", []int{1, 2, 3}, testBulkUpdateOperation{updated: &updated}, hook.TagUpdatePost, nil, []string{"favorite"})

	assert.Equal(t, 1, jobID)
	assert.Equal(t, []int{1, 2, 3}, updated)
	assert.Equal(t, []int{1, 2, 3}, hooks.calls)

	jobState := jobMgr.GetJob(jobID)
	assert.NotNil(t, jobState)
	assert.Equal(t, job.StatusFinished, jobState.Status)
}

func TestBulkUpdateJobSkipsHooksWhenDisabled(t *testing.T) {
	config.InitializeEmpty()
	config.GetInstance().SetBool(config.BulkUpdateHooks, false)
	defer config.GetInstance().SetBool(config.BulkUpdateHooks, true)

	db := mocks.NewDatabase()
	jobMgr := job.NewManager()
	jobMgr.SetSync(true)

	hooks := &testHookExecutor{}
	mgr := &Manager{
		Repository:   db.Repository(),
		JobManager:   jobMgr,
		HookExecutor: hooks,
	}

	var updated []int
	jobID := mgr.BulkUpdate(context.Background(), "Bulk test", []int{4, 5}, testBulkUpdateOperation{updated: &updated}, hook.TagUpdatePost, nil, []string{"favorite"})

	assert.Equal(t, 1, jobID)
	assert.Equal(t, []int{4, 5}, updated)
	assert.Empty(t, hooks.calls)

	jobState := jobMgr.GetJob(jobID)
	assert.NotNil(t, jobState)
	assert.Equal(t, job.StatusFinished, jobState.Status)
}

func TestBulkUpdateJobContinuesAfterItemErrorsWhenHooksEnabled(t *testing.T) {
	config.InitializeEmpty()
	config.GetInstance().SetBool(config.BulkUpdateHooks, true)

	db := mocks.NewDatabase()
	jobMgr := job.NewManager()
	jobMgr.SetSync(true)

	hooks := &testHookExecutor{}
	mgr := &Manager{
		Repository:   db.Repository(),
		JobManager:   jobMgr,
		HookExecutor: hooks,
	}

	var attempted []int
	jobID := mgr.BulkUpdate(
		context.Background(),
		"Bulk test",
		[]int{1, 2, 3},
		testBulkUpdateOperationWithFailures{
			attempted: &attempted,
			failures: map[int]error{
				2: errors.New("boom"),
			},
		},
		hook.TagUpdatePost,
		nil,
		[]string{"favorite"},
	)

	assert.Equal(t, 1, jobID)
	assert.Equal(t, []int{1, 2, 3}, attempted)
	assert.Equal(t, []int{1, 3}, hooks.calls)

	jobState := jobMgr.GetJob(jobID)
	assert.NotNil(t, jobState)
	assert.Equal(t, job.StatusFailed, jobState.Status)
	assert.Contains(t, *jobState.Error, "1 of 3 items failed: 2: boom")
	assert.Equal(t, 1.0, jobState.Progress)
}

func TestBulkUpdateJobContinuesAfterItemErrorsWhenHooksDisabled(t *testing.T) {
	config.InitializeEmpty()
	config.GetInstance().SetBool(config.BulkUpdateHooks, false)
	defer config.GetInstance().SetBool(config.BulkUpdateHooks, true)

	db := mocks.NewDatabase()
	jobMgr := job.NewManager()
	jobMgr.SetSync(true)

	hooks := &testHookExecutor{}
	mgr := &Manager{
		Repository:   db.Repository(),
		JobManager:   jobMgr,
		HookExecutor: hooks,
	}

	var attempted []int
	jobID := mgr.BulkUpdate(
		context.Background(),
		"Bulk test",
		[]int{4, 5, 6},
		testBulkUpdateOperationWithFailures{
			attempted: &attempted,
			failures: map[int]error{
				5: errors.New("boom"),
			},
		},
		hook.TagUpdatePost,
		nil,
		[]string{"favorite"},
	)

	assert.Equal(t, 1, jobID)
	assert.Equal(t, []int{4, 5, 6}, attempted)
	assert.Empty(t, hooks.calls)

	jobState := jobMgr.GetJob(jobID)
	assert.NotNil(t, jobState)
	assert.Equal(t, job.StatusFailed, jobState.Status)
	assert.Contains(t, *jobState.Error, "1 of 3 items failed: 5: boom")
	assert.Equal(t, 1.0, jobState.Progress)
}
