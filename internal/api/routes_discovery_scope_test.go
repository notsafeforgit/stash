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
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryScopeHTTPRecoversSavedPlansAfterLostReplyAndRestart(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	f := newDiscoveryHTTPFixture(t)
	directory := t.TempDir()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for range 2 {
			input := f.listing.DiscoveryListingInput
			input.UUID = uuid.NewString()
			if _, err := f.repo.DiscoveryJob.CreateListing(ctx, input, f.now); err != nil {
				return err
			}
		}
		definition := f.collection.SourceCollectionDefinition
		definition.Label = "Reviewed discovery collection"
		var err error
		f.collection, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	var handler atomic.Value
	handler.Store(http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: f.repo}).router()))
	var dropped atomic.Bool
	var applies atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-discovery-review" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.Load().(http.Handler).ServeHTTP(recorder, r)
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/discovery-scope-reviews") && recorder.Code == 200 {
			applies.Add(1)
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
	defer server.Close()
	run := func(phase string) {
		setup, err := json.Marshal(map[string]any{"phase": phase, "endpoint": server.URL, "directory": directory, "collection": f.collection.UUID})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_discovery_scope.py"), string(setup))
		command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-discovery-review")
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
	}
	run("apply")
	require.EqualValues(t, 3, applies.Load())
	require.True(t, dropped.Load())
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		// Later work and collection retirement must not invalidate old receipts.
		_, err := f.repo.DiscoveryJob.Admit(ctx, f.listing.UUID, time.Now().Add(time.Second))
		if err != nil {
			return err
		}
		definition := f.collection.SourceCollectionDefinition
		definition.State = "retired"
		_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	handler.Store(http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: f.repo}).router()))
	run("verify")
	require.EqualValues(t, 3, applies.Load(), "restart recovers receipts without another Apply")
}
