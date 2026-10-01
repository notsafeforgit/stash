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

func TestPythonCatalogIdentityImportLostResponse(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	accounts := map[string]string{}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(71,'2020-01-01 00:00:00','2020-01-01 00:00:00'); INSERT INTO performer_names(performer_id,name,position) VALUES(71,'Selected native name',0)`, nil)
		if err != nil {
			return err
		}
		for _, key := range []string{"twitter:id:9007199254740993", "reddit:handle:deliberately-unlinked"} {
			account, err := repo.SourceAccount.Create(ctx, "native:twitter", "Account")
			if err != nil {
				return err
			}
			accounts[key] = account.UUID
		}
		return nil
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
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/catalog-identity-imports") && recorder.Code == http.StatusOK && !dropped.Swap(true) {
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
	setup, err := json.Marshal(map[string]any{"directory": directory, "source": uuid.NewString(), "snapshot": snapshot, "endpoint": server.URL, "accounts": accounts})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_catalog_identity_import.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, dropped.Load())
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		performer, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Selected native name", performer.Name)
		owner, err := repo.SourceAccount.Ownership(ctx, accounts["reddit:handle:deliberately-unlinked"])
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipUnlinked, owner.State)
		return nil
	}))
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/catalog-identity-imports/not-a-uuid", 400},
		{"GET", "/catalog-identity-imports/" + uuid.NewString(), 404},
		{"GET", "/catalog-identity-imports/" + snapshot + "/records?after=-1", 400},
		{"POST", "/catalog-identity-imports/preview", 400},
		{"POST", "/catalog-identity-imports", 400},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, response.Body.String())
	}
	request := httptest.NewRequest("POST", "/catalog-identity-imports/preview", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
}
