package manager

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/bulkupdate"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/common"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type testHookExecutor struct {
	calls   []common.HookContext
	deliver func(context.Context, common.HookContext) error
}

func (h *testHookExecutor) ExecutePostHooks(context.Context, int, hook.TriggerEnum, interface{}, []string) {
}
func (h *testHookExecutor) PrepareDurablePostHook(_ context.Context, kind hook.TriggerEnum, input interface{}, fields []string) (common.HookContext, error) {
	return common.HookContext{Type: string(kind), Input: input, InputFields: fields}, nil
}
func (h *testHookExecutor) DeliverPreparedPostHook(ctx context.Context, notice common.HookContext) error {
	h.calls = append(h.calls, notice)
	if h.deliver != nil {
		return h.deliver(ctx, notice)
	}
	return nil
}

func bulkTestManager(t *testing.T, path string) (*Manager, *job.Manager) {
	t.Helper()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	jobs := job.NewManager()
	t.Cleanup(func() { require.NoError(t, jobs.Shutdown(context.Background())) })
	mgr := &Manager{Database: db, Repository: db.Repository(), JobManager: jobs}
	history := sqlite.NewLibraryJobHistory(db)
	require.NoError(t, jobs.EnableHistory(t.Context(), history, func(ctx context.Context) ([]job.Restoration, error) { return mgr.restoreBulkUpdates(ctx, history) }))
	return mgr, jobs
}

func bulkSceneFixtures(t *testing.T, repo models.Repository, count int) []int {
	t.Helper()
	var ids []int
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for range count {
			s := models.NewScene()
			s.Title = "original"
			if err := repo.Scene.Create(ctx, &s, nil); err != nil {
				return err
			}
			ids = append(ids, s.ID)
		}
		return nil
	}))
	return ids
}
func bulkIDs(ids ...int) bulkupdate.Selector {
	return func(context.Context) ([]int, error) { return ids, nil }
}
func bulkSceneCommand() bulkupdate.Scene {
	changes := models.NewScenePartial()
	changes.Title = models.NewOptionalString("updated")
	return bulkupdate.Scene{Changes: changes}
}

type partialBulkFailureStore struct {
	models.SceneReaderWriter
	failID int
}

func (s partialBulkFailureStore) UpdatePartial(ctx context.Context, id int, changes models.ScenePartial) (*models.Scene, error) {
	updated, err := s.SceneReaderWriter.UpdatePartial(ctx, id, changes)
	if err == nil && id == s.failID {
		return nil, errors.New("simulated relationship failure after scalar update")
	}
	return updated, err
}

// Real command admission and SQLite checkpoints preserve the original hook and
// partial-failure assertions, now also covering durable data and reclamation.
func TestBulkUpdateJobsCommitItemsAndBoundHooks(t *testing.T) {
	for _, hooksEnabled := range []bool{true, false} {
		for _, failItem := range []bool{true, false} {
			t.Run(fmt.Sprintf("hooks_%t_failure_%t", hooksEnabled, failItem), func(t *testing.T) {
				config.InitializeEmpty()
				config.GetInstance().SetBool(config.BulkUpdateHooks, hooksEnabled)
				t.Cleanup(func() { config.InitializeEmpty() })
				mgr, jobs := bulkTestManager(t, filepath.Join(t.TempDir(), "bulk.sqlite"))
				jobs.SetSync(true)
				ids := bulkSceneFixtures(t, mgr.Repository, 3)
				if failItem {
					mgr.Repository.Scene = partialBulkFailureStore{mgr.Repository.Scene, ids[1]}
				}
				hooks := &testHookExecutor{}
				mgr.HookExecutor = hooks
				id, total, err := mgr.BulkUpdate(t.Context(), "bulk fixture", bulkIDs(ids...), bulkSceneCommand(), hook.SceneUpdatePost, nil, []string{"title"})
				require.NoError(t, err)
				require.Equal(t, 3, total)
				_, err = uuid.Parse(id)
				require.NoError(t, err)
				state := jobs.GetJob(id)
				require.NotNil(t, state)
				require.Equal(t, 1.0, state.Progress)
				if failItem {
					require.Equal(t, job.StatusFailed, state.Status)
					require.Contains(t, *state.Error, "1 of 3 items failed")
					require.Contains(t, *state.Error, "simulated relationship failure")
				} else {
					require.Equal(t, job.StatusFinished, state.Status)
				}
				require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
					for i, id := range ids {
						value, err := mgr.Repository.Scene.Find(ctx, id)
						if err != nil {
							return err
						}
						want := "updated"
						if failItem && i == 1 {
							want = "original"
						}
						require.Equal(t, want, value.Title)
					}
					saved, err := mgr.Repository.LibraryBulkJob.Find(ctx, id)
					require.NoError(t, err)
					require.Nil(t, saved, "terminal jobs release commands and all targets")
					return nil
				}))
				wantHooks := 0
				if hooksEnabled {
					wantHooks = 3
					if failItem {
						wantHooks--
					}
				}
				require.Len(t, hooks.calls, wantHooks)
				for _, notice := range hooks.calls {
					require.NotEmpty(t, notice.UUID)
					require.NotEmpty(t, notice.EventID)
					require.Equal(t, string(hook.SceneUpdatePost), notice.Type)
					if failItem {
						require.NotEqual(t, ids[1], notice.ID)
					}
				}
			})
		}
	}
}

func waitBulkTerminal(t *testing.T, jobs *job.Manager, id string, want job.Status) {
	t.Helper()
	require.Eventually(t, func() bool { state := jobs.GetJob(id); return state != nil && state.EndTime != nil }, 10*time.Second, 10*time.Millisecond)
	state := jobs.GetJob(id)
	require.Equal(t, want, state.Status, "%+v", state)
}
