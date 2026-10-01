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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonScanJournalImporterRetainsAtomicSnapshotAfterLostResponse(t *testing.T) {
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
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Frozen input", State: "disabled"}})
		return err
	}))
	router := (&nativeArchiveRoutes{repo: repo}).router()
	handler := http.StripPrefix("/api/v3/archive", router)
	var dropped atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if r.Method == "POST" && recorder.Code == http.StatusOK && !dropped.Swap(true) {
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
	snapshot := uuid.NewString()
	setup, err := json.Marshal(map[string]string{"directory": directory, "root": root.UUID, "source": uuid.NewString(), "snapshot": snapshot, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_scan_journal_import.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, dropped.Load())
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, table := range []string{"scan_journals", "scan_journal_records", "source_runs", "source_run_attempts", "source_backfill_decisions"} {
			_, rows, err := db.QuerySQL(ctx, "SELECT count(*) FROM "+table, nil)
			require.NoError(t, err)
			wanted := int64(0)
			switch table {
			case "scan_journals":
				wanted = 1
			case "scan_journal_records":
				wanted = 8
			}
			require.Equal(t, wanted, rows[0][0])
		}
		return nil
	}))
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/scan-journals/not-a-uuid", http.StatusBadRequest},
		{"GET", "/scan-journals/" + uuid.NewString(), http.StatusNotFound},
		{"GET", "/scan-journals/" + snapshot + "/records?after=-1", http.StatusBadRequest},
		{"GET", "/scan-journals/" + snapshot + "/records?table=unknown", http.StatusBadRequest},
		{"POST", "/scan-journals/import", http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(test.method, test.path, strings.NewReader(`{}`)))
		require.Equal(t, test.status, w.Code, w.Body.String())
	}
	w := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/scan-journals/import", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://another.invalid")
	router.ServeHTTP(w, request)
	require.Equal(t, http.StatusForbidden, w.Code)
	w = httptest.NewRecorder()
	(&ingestRoutes{service: ingest.New(repo)}).router().ServeHTTP(w, httptest.NewRequest("POST", ingestPath+"/scan-journals/import", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusNotFound, w.Code)
}
