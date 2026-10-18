package manager

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type failedReset struct{}

func (failedReset) Reset() error { return errors.New("reset failed") }

func TestImportJobReportsFailures(t *testing.T) {
	for _, kind := range []string{"archive", "reset", "records", "success"} {
		t.Run(kind, func(t *testing.T) {
			task := &ImportTask{BaseDir: t.TempDir()}
			switch kind {
			case "archive":
				task.TmpZip = filepath.Join(t.TempDir(), "missing.zip")
			case "reset":
				task.Reset, task.resetter = true, failedReset{}
			case "records":
				dir := filepath.Join(task.BaseDir, "tags")
				require.NoError(t, os.Mkdir(dir, 0700))
				for _, name := range []string{"one.json", "two.json"} {
					require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("invalid JSON"), 0600))
				}
			}
			jobs := job.NewManager()
			t.Cleanup(jobs.Stop)
			jobs.SetSync(true)
			manager := Manager{JobManager: jobs}
			id := manager.RunSingleTask(t.Context(), task)
			result := jobs.GetJob(id)
			require.NotNil(t, result)
			if kind == "success" {
				require.Equal(t, job.StatusFinished, result.Status)
				require.Nil(t, result.Error)
			} else {
				require.Equal(t, job.StatusFailed, result.Status)
				require.NotNil(t, result.Error)
				if kind == "records" {
					require.Contains(t, *result.Error, "2 operation(s) failed")
				}
			}
		})
	}
}

func TestExportJobReportsQueryFailure(t *testing.T) {
	config.InitializeEmpty()
	config.GetInstance().SetString(config.Metadata, t.TempDir())
	db := mocks.NewDatabase()
	db.Scene.On("All", mock.Anything).Return(nil, errors.New("query failed"))
	for _, reader := range []*mock.Mock{&db.Image.Mock, &db.Gallery.Mock, &db.Group.Mock, &db.Performer.Mock, &db.Studio.Mock, &db.Tag.Mock, &db.SavedFilter.Mock} {
		reader.On("All", mock.Anything).Return(nil, nil)
	}
	jobs := job.NewManager()
	t.Cleanup(jobs.Stop)
	jobs.SetSync(true)
	manager := Manager{JobManager: jobs, Repository: db.Repository()}
	id, err := manager.Export(t.Context())
	require.NoError(t, err, "scheduling succeeds; execution reports its failure")
	result := jobs.GetJob(id)
	require.Equal(t, job.StatusFailed, result.Status)
	require.NotNil(t, result.Error)
	require.Contains(t, *result.Error, "query failed")
	db.AssertExpectations(t)
}

func TestExportReportsDirectoryAndArchiveFailures(t *testing.T) {
	base := t.TempDir()
	task := ExportTask{json: jsonUtils{json: *paths.GetJSONPaths(base)}}
	require.NoError(t, task.prepareDirectories())
	var archive bytes.Buffer
	require.NoError(t, task.zipFiles(&archive))
	require.Error(t, task.zipFiles(failingExportWriter{}), "ZIP central-directory write errors must propagate")
	require.NoError(t, os.RemoveAll(task.json.json.Scenes))
	require.Error(t, task.zipFiles(&archive), "directory traversal errors must propagate")
	require.NoError(t, os.WriteFile(task.json.json.Scenes, []byte("blocked"), 0600))
	require.Error(t, task.prepareDirectories())
}

type failingExportWriter struct{}

func (failingExportWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestImportExportHonourCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, (&ImportTask{}).Start(ctx), context.Canceled)
	require.ErrorIs(t, (&ExportTask{}).Start(ctx), context.Canceled)
}

func TestTaskFailuresCollectConcurrentWorkers(t *testing.T) {
	var failures taskFailures
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() { failures.addf("write failed") })
	}
	workers.Wait()
	require.ErrorContains(t, failures.err(), "20 operation(s) failed")
}
