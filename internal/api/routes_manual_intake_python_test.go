package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonFolderIntakeRecoversAdmissionWithoutStackingJobs(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory, media := t.TempDir(), t.TempDir()
	for _, name := range []string{"first.mp4", "second.jpg"} {
		require.NoError(t, os.WriteFile(filepath.Join(media, name), []byte("waiting for the file worker"), 0600))
	}
	binding, err := archive.ProbeMediaRoot(media)
	require.NoError(t, err)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var root *models.MediaRoot
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{
			Label: "Local files", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Purchases", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
		return err
	}))
	var handlerMu sync.RWMutex
	handler := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo, fileIngestion: true}).router())
	var lost atomic.Bool
	var applies atomic.Int32
	var original atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-private-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handlerMu.RLock()
		handler.ServeHTTP(recorder, r)
		handlerMu.RUnlock()
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/apply") {
			applies.Add(1)
			if recorder.Code == http.StatusAccepted && !lost.Swap(true) {
				var accepted ingest.ManualFileStatus
				if err := json.Unmarshal(recorder.Body.Bytes(), &accepted); err != nil {
					t.Error(err)
					return
				}
				original.Store(accepted)
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
				return
			}
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	run := func(phase string) {
		setup, err := json.Marshal(map[string]string{"directory": directory, "endpoint": server.URL,
			"root_uuid": root.UUID, "collection_uuid": collection.UUID, "phase": phase})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_intake_folder.py"), string(setup))
		command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "PYTHONDONTWRITEBYTECODE=1")
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	run("lost")
	require.EqualValues(t, 1, applies.Load())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	handlerMu.Lock()
	handler = http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo, fileIngestion: false}).router())
	handlerMu.Unlock()
	run("recovered")
	require.EqualValues(t, 1, applies.Load(), "recovery cannot submit another request, including while the worker is unavailable")
	accepted := original.Load().(ingest.ManualFileStatus)
	_, err = ingest.New(repo).CancelManualFile(t.Context(), accepted.RequestUUID, accepted.Revision)
	require.NoError(t, err)
	handlerMu.Lock()
	handler = http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo, fileIngestion: true}).router())
	handlerMu.Unlock()
	run("next")
	require.EqualValues(t, 2, applies.Load(), "only the second path may be admitted after cancellation")
}
