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
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonCatalogRegistryImportLostResponse(t *testing.T) {
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
	var dropped atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/catalog-registry-imports") && recorder.Code == http.StatusOK && !dropped.Swap(true) {
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
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_catalog_registry_import.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, dropped.Load())
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		performer, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Selected name", performer.Name)
		accounts, err := repo.SourceAccount.Lookup(ctx, models.AccountReference{Namespace: "native:reddit", Kind: "handle", Value: "deliberately-unlinked"}, "", 100)
		require.NoError(t, err)
		require.Len(t, accounts, 1)
		owner, err := repo.SourceAccount.Ownership(ctx, accounts[0].UUID)
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipUnlinked, owner.State)
		return nil
	}))
	for _, test := range []struct {
		method, path string
		status       int
	}{{"GET", "/catalog-registry-imports/not-a-uuid", 400}, {"GET", "/catalog-registry-imports/" + uuid.NewString(), 404}, {"GET", "/catalog-registry-imports/" + snapshot + "/records?after=-1", 400}, {"POST", "/catalog-registry-imports/preview", 400}, {"POST", "/catalog-registry-imports", 400}} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, response.Body.String())
	}
	request := httptest.NewRequest("POST", "/catalog-registry-imports/preview", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
}
