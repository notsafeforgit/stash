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

func TestPythonMetadataPolicyImportLostResponse(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "migration",
			SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Retained folder", Kind: "directory", State: "disabled"}})
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
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/metadata-policy-imports") && recorder.Code == http.StatusOK && !dropped.Swap(true) {
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
	requestID := uuid.NewString()
	setup, err := json.Marshal(map[string]any{"directory": directory, "uuid": requestID, "collection": collection.UUID, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_metadata_policy_import.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, dropped.Load())
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		details, err := repo.MetadataPolicyImport.Find(ctx, requestID)
		require.NoError(t, err)
		require.Equal(t, 1, details.PolicyRevision)
		require.Equal(t, []string{"python/set_organized_only_if"}, details.ReviewKeys)
		policy, err := repo.MetadataPolicy.Find(ctx, collection.UUID)
		require.NoError(t, err)
		require.False(t, policy.Definition.Enabled)
		return nil
	}))
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/metadata-policy-imports/not-a-uuid", 400},
		{http.MethodGet, "/metadata-policy-imports/" + uuid.NewString(), 404},
		{http.MethodGet, "/collections/" + collection.UUID + "/metadata-policy-imports?limit=101", 400},
		{http.MethodPost, "/metadata-policy-imports/preview", 400},
		{http.MethodPost, "/metadata-policy-imports", 400},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, test.status, response.Code, response.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/metadata-policy-imports/preview", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
}
