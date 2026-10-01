package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonBackfillImporterReplaysAfterLostNativeResponse(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var root *models.MediaRoot
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Import copy", State: "disabled"}})
		return err
	}))
	handler := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	var dropped atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if recorder.Code == http.StatusOK && !dropped.Swap(true) {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	defer server.Close()
	setup, err := json.Marshal(map[string]string{"directory": directory, "root": root.UUID, "source": uuid.NewString(), "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_backfill_import.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, dropped.Load())
	require.Contains(t, string(output), `"decisions": 52`)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		status, err := repo.SourceBackfill.Status(ctx, models.BackfillSubject{RootUUID: root.UUID, Platform: "twitter", Account: "51"}, "twitter")
		require.NoError(t, err)
		require.Equal(t, "completed", status.State)
		require.True(t, status.AccountComplete)
		status, err = repo.SourceBackfill.Status(ctx, models.BackfillSubject{RootUUID: root.UUID, Platform: "reddit", Account: "Skipped_Account"}, "reddit-new")
		require.NoError(t, err)
		require.Equal(t, "skipped", status.State)
		require.False(t, status.AccountComplete)
		for _, table := range []string{"source_backfill_decisions", "source_runs", "source_backfill_requests"} {
			_, rows, err := db.QuerySQL(ctx, "SELECT count(*) AS count FROM "+table, nil)
			require.NoError(t, err)
			wanted := int64(0)
			if table == "source_backfill_decisions" {
				wanted = 52
			}
			require.Equal(t, wanted, rows[0][0])
		}
		return nil
	}))
}
