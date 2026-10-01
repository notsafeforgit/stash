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
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonCatalogSnapshotUploadResumesLostResponses(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(71,'2020-01-01 00:00:00','2020-01-01 00:00:00'); INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Selected name',0)`, nil)
		return err
	}))
	router := (&nativeArchiveRoutes{repo: repo}).router()
	handler := http.StripPrefix("/api/v3/archive", router)
	var lostBegin, lostChunk atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		drop := recorder.Code == 200 && ((r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/catalog-snapshots") && !lostBegin.Swap(true)) ||
			(r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/chunks/0") && !lostChunk.Swap(true)))
		if drop {
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
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": uuid.NewString(), "snapshot": snapshot, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_catalog_upload.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lostBegin.Load())
	require.True(t, lostChunk.Load())
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		receipt, err := repo.CatalogSnapshot.Find(ctx, snapshot)
		require.NoError(t, err)
		require.Equal(t, "received", receipt.State)
		require.False(t, receipt.Imported)
		performer, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Selected name", performer.Name)
		return nil
	}))
	for _, test := range []struct {
		method, path, contentType string
		status                    int
	}{
		{"GET", "/catalog-snapshots/" + snapshot, "", 200},
		{"GET", "/catalog-snapshots/not-a-uuid", "", 400},
		{"GET", "/catalog-snapshots/" + uuid.NewString(), "", 404},
		{"POST", "/catalog-snapshots", "application/json", 400},
		{"POST", "/catalog-snapshots", "text/plain", 400},
		{"PUT", "/catalog-snapshots/" + snapshot + "/chunks/-1", "application/x-ndjson", 400},
		{"PUT", "/catalog-snapshots/" + snapshot + "/chunks/0", "application/json", 400},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
		request.Header.Set("Content-Type", test.contentType)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, response.Body.String())
		if test.status == 200 {
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		}
	}
	request := httptest.NewRequest("POST", "/catalog-snapshots", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, 403, response.Code)
}
