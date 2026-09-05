//go:build integration

package manager

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/sqlite"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
)

type partialBulkFailure struct {
	scenes models.SceneReaderWriter
	failID int
}

func (o partialBulkFailure) Update(ctx context.Context, id int) error {
	partial := models.NewScenePartial()
	partial.Title = models.NewOptionalString("partially updated")
	if _, err := o.scenes.UpdatePartial(ctx, id, partial); err != nil {
		return err
	}
	if id == o.failID {
		return errors.New("simulated relationship failure after scalar update")
	}
	return nil
}

func TestBulkFailureIsAtomicAndVisible(t *testing.T) {
	for _, hooks := range []bool{true, false} {
		t.Run(fmt.Sprintf("hooks_%t", hooks), func(t *testing.T) {
			config.InitializeEmpty()
			config.GetInstance().SetBool(config.BulkUpdateHooks, hooks)
			db := sqlite.NewDatabase()
			if err := db.Open(filepath.Join(t.TempDir(), "bulk.sqlite")); err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			repo := db.Repository()
			scene := models.NewScene()
			scene.Title = "original"
			if err := repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Scene.Create(ctx, &scene, nil) }); err != nil {
				t.Fatal(err)
			}
			successful := models.NewScene()
			successful.Title = "original"
			if err := repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Scene.Create(ctx, &successful, nil) }); err != nil {
				t.Fatal(err)
			}
			jobs := job.NewManager()
			jobs.SetSync(true)
			mgr := &Manager{Repository: repo, JobManager: jobs}
			id := mgr.BulkUpdate(context.Background(), "audit failure", []int{scene.ID, successful.ID}, partialBulkFailure{repo.Scene, scene.ID}, hook.SceneUpdatePost, nil, nil)
			state := jobs.GetJob(id)
			t.Logf("job reported status: %s", state.Status)
			if state.Status != job.StatusFailed || state.Error == nil {
				t.Error("failed item was not reported as a failed job with an error")
			}
			if err := repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
				got, err := repo.Scene.Find(ctx, scene.ID)
				if err != nil {
					return err
				}
				t.Logf("persisted scene title: %q", got.Title)
				if got.Title != "original" {
					t.Errorf("failed item's scalar write was committed: %q", got.Title)
				}
				succeeded, err := repo.Scene.Find(ctx, successful.ID)
				if err != nil {
					return err
				}
				if succeeded.Title != "partially updated" {
					t.Error("successful item was not committed")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
