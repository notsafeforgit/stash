package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

func admitBulkFixture(t *testing.T, mgr *Manager, selection bulkupdate.Selector, operation bulkupdate.Operation) string {
	t.Helper()
	command, err := bulkupdate.Encode(operation)
	require.NoError(t, err)
	executor := &bulkUpdateJob{repo: mgr.Repository, services: mgr.bulkServices(), hookExecutor: mgr.HookExecutor, command: command, selector: selection, hookType: hook.SceneUpdatePost}
	id := uuid.NewString()
	require.NoError(t, executor.Admit(t.Context(), job.Job{ID: id, Generation: 1, Description: "recovery fixture", AddTime: time.Now()}))
	return id
}

func resumeBulkFixture(t *testing.T, mgr *Manager) *job.Manager {
	t.Helper()
	jobs := job.NewManager()
	t.Cleanup(func() { require.NoError(t, jobs.Shutdown(context.Background())) })
	mgr.JobManager = jobs
	history := sqlite.NewLibraryJobHistory(mgr.Database)
	require.NoError(t, jobs.EnableHistory(t.Context(), history, func(ctx context.Context) ([]job.Restoration, error) { return mgr.restoreBulkUpdates(ctx, history) }))
	return jobs
}

type crashBulkCheckpoint struct {
	models.LibraryBulkJobReaderWriter
}

func (s crashBulkCheckpoint) Applied(ctx context.Context, id string, generation int, target models.LibraryBulkTarget) (int, error) {
	processed, err := s.LibraryBulkJobReaderWriter.Applied(ctx, id, generation, target)
	if err == nil {
		txn.AddPostCommitHook(ctx, func(context.Context) { os.Exit(76) })
	}
	return processed, err
}

func TestBulkCrashHelper(t *testing.T) {
	path := os.Getenv("STASH_BULK_CRASH_DATABASE")
	if path == "" {
		return
	}
	config.InitializeEmpty()
	config.GetInstance().SetBool(config.BulkUpdateHooks, true)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	mgr := &Manager{Database: db, Repository: db.Repository()}
	hooks := &testHookExecutor{}
	mgr.HookExecutor = hooks
	switch os.Getenv("STASH_BULK_CRASH_PHASE") {
	case "after_edit":
		mgr.Repository.LibraryBulkJob = crashBulkCheckpoint{mgr.Repository.LibraryBulkJob}
	case "after_delivery":
		hooks.deliver = func(_ context.Context, notice common.HookContext) error {
			data, err := json.Marshal(notice)
			require.NoError(t, err)
			f, err := os.OpenFile(path+".notice", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			require.NoError(t, err)
			_, err = f.Write(data)
			require.NoError(t, err)
			require.NoError(t, f.Sync())
			require.NoError(t, f.Close())
			os.Exit(76)
			return nil
		}
	default:
		t.Fatal("unknown crash fixture phase")
	}
	jobs := resumeBulkFixture(t, mgr)
	require.Eventually(t, func() bool { return len(jobs.GetQueue()) == 0 }, 10*time.Second, 10*time.Millisecond)
	t.Fatal("crash checkpoint was not reached")
}

func TestBulkRecoveryAfterProcessExitDoesNotRepeatEdits(t *testing.T) {
	for _, phase := range []string{"after_edit", "after_delivery"} {
		t.Run(phase, func(t *testing.T) {
			config.InitializeEmpty()
			config.GetInstance().SetBool(config.BulkUpdateHooks, true)
			t.Cleanup(func() { config.InitializeEmpty() })
			path := filepath.Join(t.TempDir(), "bulk.sqlite")
			db := sqlite.NewDatabase()
			require.NoError(t, db.Open(path))
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			mgr := &Manager{Database: db, Repository: db.Repository(), HookExecutor: &testHookExecutor{}}
			ids := bulkSceneFixtures(t, mgr.Repository, 3)
			op := bulkSceneCommand()
			date, err := models.ParseDate("2001")
			require.NoError(t, err)
			op.Changes.ProductionDate = models.NewOptionalDate(date)
			op.Changes.Details = models.OptionalString{Set: true, Null: true}
			id := admitBulkFixture(t, mgr, bulkIDs(ids...), op)
			require.NoError(t, db.Close())
			binary, err := os.Executable()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestBulkCrashHelper$")
			cmd.Env = append(os.Environ(), "STASH_BULK_CRASH_DATABASE="+path, "STASH_BULK_CRASH_PHASE="+phase)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, "%s", output)
			require.Equal(t, 76, exit.ExitCode(), "%s", output)
			require.NoError(t, db.Open(path))
			mgr.Repository = db.Repository()
			require.NoError(t, mgr.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
				saved, err := mgr.Repository.LibraryBulkJob.Find(ctx, id)
				require.NoError(t, err)
				require.Equal(t, 1, saved.Processed)
				s, err := mgr.Repository.Scene.Find(ctx, ids[0])
				require.NoError(t, err)
				require.Equal(t, "updated", s.Title)
				patch := models.NewScenePartial()
				patch.Title = models.NewOptionalString("manual edit after interruption")
				_, err = mgr.Repository.Scene.UpdatePartial(ctx, ids[0], patch)
				return err
			}))
			hooks := &testHookExecutor{}
			mgr.HookExecutor = hooks
			jobs := resumeBulkFixture(t, mgr)
			waitBulkTerminal(t, jobs, id, job.StatusFinished)
			require.Len(t, hooks.calls, 3)
			if phase == "after_delivery" {
				data, err := os.ReadFile(path + ".notice")
				require.NoError(t, err)
				var first common.HookContext
				require.NoError(t, json.Unmarshal(data, &first))
				require.Equal(t, first.EventID, hooks.calls[0].EventID, "retry must retain its original event ID")
				require.Equal(t, first.UUID, hooks.calls[0].UUID)
			}
			require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				for i, id := range ids {
					s, err := mgr.Repository.Scene.Find(ctx, id)
					require.NoError(t, err)
					want := "updated"
					if i == 0 {
						want = "manual edit after interruption"
					}
					require.Equal(t, want, s.Title)
					require.NotNil(t, s.ProductionDate)
					require.Equal(t, models.DatePrecisionYear, s.ProductionDate.Precision)
					require.Empty(t, s.Details)
				}
				saved, err := mgr.Repository.LibraryBulkJob.Find(ctx, id)
				require.NoError(t, err)
				require.Nil(t, saved)
				return nil
			}))
			require.NoError(t, jobs.Shutdown(t.Context()))
			again := resumeBulkFixture(t, mgr)
			require.Empty(t, again.GetQueue())
			state, err := again.Find(t.Context(), id)
			require.NoError(t, err)
			require.Equal(t, job.StatusFinished, state.Status)
		})
	}
}

func TestBulkRecoveryFreezesSelectionAndRejectsRetiredTargets(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	for _, retire := range []string{"none", "target", "relationship"} {
		t.Run(retire, func(t *testing.T) {
			db := sqlite.NewDatabase()
			require.NoError(t, db.Open(filepath.Join(t.TempDir(), "bulk.sqlite")))
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			mgr := &Manager{Database: db, Repository: db.Repository()}
			ids := bulkSceneFixtures(t, mgr.Repository, 2)
			op := bulkSceneCommand()
			tag := models.NewTag()
			tag.Name = "referenced tag"
			if retire == "relationship" {
				require.NoError(t, mgr.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
					return mgr.Repository.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag})
				}))
				op.Changes.TagIDs = &models.UpdateIDs{IDs: []int{tag.ID}, Mode: models.RelationshipUpdateModeAdd}
			}
			query := "original"
			selection := bulkupdate.SelectAll(mgr.Repository.Scene.QueryAST, nil, &models.FindFilterType{Q: &query}, func(s *models.Scene) int { return s.ID })
			id := admitBulkFixture(t, mgr, selection, op)
			var newcomer models.Scene
			require.NoError(t, mgr.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
				if retire == "target" {
					require.NoError(t, mgr.Repository.Scene.Destroy(ctx, ids[1]))
				}
				if retire == "relationship" {
					require.NoError(t, mgr.Repository.Tag.Destroy(ctx, tag.ID))
					_, _, err := db.ExecSQL(ctx, "INSERT INTO tags(id,name,created_at,updated_at) VALUES(?,'replacement',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)", []interface{}{tag.ID})
					require.NoError(t, err)
					replacement, err := mgr.Repository.Tag.Find(ctx, tag.ID)
					require.NoError(t, err)
					require.Equal(t, "replacement", replacement.Name, "fixture must actually reuse the local key")
				}
				newcomer = models.NewScene()
				newcomer.Title = "original"
				if retire == "target" {
					// Normal allocation avoids reuse, but an importer/operator can
					// explicitly assign a retired local key. UUIDs must still win.
					newcomer.ID = ids[1]
					_, _, err := db.ExecSQL(ctx, "INSERT INTO scenes(id,title,created_at,updated_at) VALUES(?,'original',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)", []interface{}{newcomer.ID})
					require.NoError(t, err)
				} else {
					require.NoError(t, mgr.Repository.Scene.Create(ctx, &newcomer, nil))
				}
				partial := models.NewScenePartial()
				partial.Title = models.NewOptionalString("no longer matches")
				_, err := mgr.Repository.Scene.UpdatePartial(ctx, ids[0], partial)
				return err
			}))
			jobs := resumeBulkFixture(t, mgr)
			want := job.StatusFinished
			if retire != "none" {
				want = job.StatusFailed
			}
			waitBulkTerminal(t, jobs, id, want)
			require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				first, err := mgr.Repository.Scene.Find(ctx, ids[0])
				require.NoError(t, err)
				title := "updated"
				if retire == "relationship" {
					title = "no longer matches"
				}
				require.Equal(t, title, first.Title)
				newScene, err := mgr.Repository.Scene.Find(ctx, newcomer.ID)
				require.NoError(t, err)
				require.Equal(t, "original", newScene.Title)
				return nil
			}))
		})
	}
}

func TestBulkShutdownResumesButExplicitCancellationDoesNot(t *testing.T) {
	for _, cancelExplicitly := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit_cancel_%t", cancelExplicitly), func(t *testing.T) {
			config.InitializeEmpty()
			config.GetInstance().SetBool(config.BulkUpdateHooks, true)
			t.Cleanup(func() { config.InitializeEmpty() })
			mgr, jobs := bulkTestManager(t, filepath.Join(t.TempDir(), "bulk.sqlite"))
			ids := bulkSceneFixtures(t, mgr.Repository, 3)
			started := make(chan struct{})
			mgr.HookExecutor = &testHookExecutor{deliver: func(ctx context.Context, _ common.HookContext) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}}
			first, _, err := mgr.BulkUpdate(t.Context(), "running", bulkIDs(ids[:2]...), bulkSceneCommand(), hook.SceneUpdatePost, nil, nil)
			require.NoError(t, err)
			<-started
			second, _, err := mgr.BulkUpdate(t.Context(), "queued", bulkIDs(ids[2]), bulkSceneCommand(), hook.SceneUpdatePost, nil, nil)
			require.NoError(t, err)
			if cancelExplicitly {
				require.NoError(t, jobs.CancelJob(second))
				require.NoError(t, jobs.CancelJob(first))
			}
			require.NoError(t, jobs.Shutdown(t.Context()))
			history := sqlite.NewLibraryJobHistory(mgr.Database)
			for _, id := range []string{first, second} {
				state, err := history.Find(t.Context(), id)
				require.NoError(t, err)
				if cancelExplicitly {
					require.Equal(t, job.StatusCancelled, state.Status)
					require.NotNil(t, state.EndTime)
				} else {
					require.Equal(t, job.StatusReady, state.Status)
					require.Nil(t, state.EndTime)
				}
			}
			// Policy is captured at admission; changing it cannot erase a
			// committed edit's pending notification during recovery.
			config.GetInstance().SetBool(config.BulkUpdateHooks, false)
			hooks := &testHookExecutor{}
			mgr.HookExecutor = hooks
			resumed := resumeBulkFixture(t, mgr)
			if cancelExplicitly {
				require.Empty(t, resumed.GetQueue())
			} else {
				waitBulkTerminal(t, resumed, first, job.StatusFinished)
				waitBulkTerminal(t, resumed, second, job.StatusFinished)
			}
			require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				for i, id := range ids {
					scene, err := mgr.Repository.Scene.Find(ctx, id)
					require.NoError(t, err)
					title := "updated"
					if cancelExplicitly && i > 0 {
						title = "original"
					}
					require.Equal(t, title, scene.Title)
				}
				return nil
			}))
			if cancelExplicitly {
				require.Empty(t, hooks.calls)
			} else {
				require.Len(t, hooks.calls, 3)
			}
		})
	}
}

func TestBulkUnknownCommandDoesNotExecuteOrBlockOtherJobs(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "bulk.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	mgr := &Manager{Database: db, Repository: db.Repository()}
	ids := bulkSceneFixtures(t, mgr.Repository, 2)
	unknown := admitBulkFixture(t, mgr, bulkIDs(ids[0]), bulkSceneCommand())
	valid := admitBulkFixture(t, mgr, bulkIDs(ids[1]), bulkSceneCommand())
	require.NoError(t, mgr.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, "UPDATE library_bulk_jobs SET command_version=99 WHERE job_id=?", []interface{}{unknown})
		return err
	}))
	jobs := resumeBulkFixture(t, mgr)
	waitBulkTerminal(t, jobs, valid, job.StatusFinished)
	failed, err := jobs.Find(t.Context(), unknown)
	require.NoError(t, err)
	require.Equal(t, job.StatusFailed, failed.Status)
	require.Contains(t, *failed.Error, "unsupported bulk command version 99")
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		scene, err := mgr.Repository.Scene.Find(ctx, ids[0])
		require.NoError(t, err)
		require.Equal(t, "original", scene.Title)
		return nil
	}))
}

func TestBulkNotificationRetriesAreBoundedAndLaterItemsContinue(t *testing.T) {
	config.InitializeEmpty()
	config.GetInstance().SetBool(config.BulkUpdateHooks, true)
	t.Cleanup(func() { config.InitializeEmpty() })
	mgr, jobs := bulkTestManager(t, filepath.Join(t.TempDir(), "bulk.sqlite"))
	jobs.SetSync(true)
	ids := bulkSceneFixtures(t, mgr.Repository, 2)
	hooks := &testHookExecutor{deliver: func(_ context.Context, n common.HookContext) error {
		if n.ID == ids[0] {
			return fmt.Errorf("plugin unreachable")
		}
		return nil
	}}
	mgr.HookExecutor = hooks
	id, _, err := mgr.BulkUpdate(t.Context(), "notification fixture", bulkIDs(ids...), bulkSceneCommand(), hook.SceneUpdatePost, nil, nil)
	require.NoError(t, err)
	state := jobs.GetJob(id)
	require.Equal(t, job.StatusFailed, state.Status)
	require.Contains(t, *state.Error, "0 of 2 items failed; 1 notification failures")
	require.Len(t, hooks.calls, 4)
	require.Equal(t, hooks.calls[0].EventID, hooks.calls[1].EventID)
	require.Equal(t, hooks.calls[0].EventID, hooks.calls[2].EventID)
	require.Equal(t, ids[1], hooks.calls[3].ID)
}
