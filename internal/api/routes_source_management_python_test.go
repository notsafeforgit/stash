package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonSourceManagementPreservesOwnershipAndRecoversLostReplies(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "source-management.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	service := ingest.New(repo)
	var root *models.MediaRoot
	var producer *models.IngestProducer
	var existing *models.SourceCollection
	targets := []string{"https://www.reddit.com/user/example/", "https://www.reddit.com/user/another/"}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Fixture root", State: "active"}})
		require.NoError(t, err)
		producer, err = repo.Ingest.CreateProducer(ctx, "Fixture source management")
		require.NoError(t, err)
		existing, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Owner label", Kind: "legacy_catalog", Namespace: "native:reddit", State: "active", TargetURL: targets[0], RootUUID: &root.UUID, PathPrefix: "Original folder"}})
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	application := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	producerRoutes := (&ingestRoutes{service: service}).router()
	var dropped atomic.Bool
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, ingestPath+"/") {
			if r.Header.Get("ApiKey") != "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			producerRoutes.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		application.ServeHTTP(recorder, r)
		if r.Method == http.MethodPut && recorder.Code == http.StatusOK {
			puts.Add(1)
			if !dropped.Swap(true) {
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
	t.Cleanup(server.Close)
	run := func(phase string) {
		t.Helper()
		setup, err := json.Marshal(map[string]any{"directory": directory, "root": root.UUID, "producer": producer.UUID, "endpoint": server.URL, "targets": targets, "phase": phase})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_source_management.py"))
		command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key", "STASH_INGEST_TOKEN="+token)
		command.Stdin = bytes.NewReader(setup)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		require.Contains(t, string(output), `"verified": true`)
	}
	run("create_lost_reply")
	require.True(t, dropped.Load())
	require.EqualValues(t, 1, puts.Load())
	run("resume_and_disable")
	require.EqualValues(t, 3, puts.Load(), "one creation and two disables; replay must not write again")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		rows, err := repo.SourceCollection.List(ctx, "", 10)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		for _, row := range rows {
			require.Equal(t, "disabled", row.State)
			require.Equal(t, 2, row.Revision)
			require.Nil(t, row.AccountUUID, "source registration must not infer a publisher or depicted performer")
			if row.UUID == existing.UUID {
				require.Equal(t, "Owner label", row.Label)
				require.Equal(t, "Original folder", row.PathPrefix)
				definition := row.SourceCollectionDefinition
				definition.State, definition.Label = "active", "Later owner choice"
				_, err := repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: row.UUID, ExpectedRevision: row.Revision, Origin: "review", Reason: "Later edit", SourceCollectionDefinition: definition})
				require.NoError(t, err)
			}
		}
		return nil
	}))
	run("preserve_later_edit")
	require.EqualValues(t, 3, puts.Load())
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceCollection.Find(ctx, existing.UUID)
		require.NoError(t, err)
		require.Equal(t, 3, current.Revision)
		require.Equal(t, "active", current.State)
		require.Equal(t, "Later owner choice", current.Label)
		return nil
	}))
}
