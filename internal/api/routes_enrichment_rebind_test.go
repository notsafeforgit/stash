package api

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestEnrichmentRebindHTTPRecoversLostResponseAndRestores(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "native.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var collection *models.SourceCollection
	var targets []*models.EnrichmentTarget
	now := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "HTTP review feed", Kind: "feed", State: "active", Namespace: "native:twitter", TargetURL: "https://x.com/example"}})
		if err != nil {
			return err
		}
		for i := range 105 {
			post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: fmt.Sprint(1000 + i)}, "")
			if err != nil {
				return err
			}
			url, err := repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: models.SourcePostEvidence{
				UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "migration", Basis: "catalog-row", ObservedAt: now, Details: json.RawMessage(`{}`)},
				URL: fmt.Sprintf("https://x.com/example/status/%d", 1000+i)})
			if err != nil {
				return err
			}
			target, err := repo.EnrichmentWork.RetainTarget(ctx, models.EnrichmentTargetInput{PostUUID: post.UUID, URLUUID: url.URLUUID,
				CollectionUUID: collection.UUID, CollectionRevision: 1, Policy: models.EnrichmentGalleryMetadataV1, Origin: "review"},
				models.EnrichmentSchedule{State: "pending", Priority: i % 101, NotBefore: now.Add(time.Hour)}, now)
			if err != nil {
				return err
			}
			targets = append(targets, target)
		}
		definition := collection.SourceCollectionDefinition
		definition.Label = "Reviewed current feed"
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: 1, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	handler := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	var lost atomic.Bool
	var applies atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		if recorder.Code == 200 && r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/enrichment-rebindings") {
			applies.Add(1)
			if !lost.Swap(true) {
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
	setup, err := json.Marshal(map[string]any{"directory": directory, "collection": collection.UUID, "endpoint": server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests", "http_enrichment_rebind.py"), string(setup))
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.True(t, lost.Load())
	require.EqualValues(t, 2, applies.Load(), "receipt-first recovery does not resubmit a committed batch")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, original := range targets {
			current, err := repo.EnrichmentWork.Target(ctx, original.UUID)
			require.NoError(t, err)
			require.Equal(t, "excluded", current.State)
			require.Equal(t, 3, current.Revision)
			require.Equal(t, original.Priority, current.Priority)
			require.True(t, original.NotBefore.Equal(current.NotBefore))
		}
		for _, state := range []string{"queued", "running", "succeeded", "failed", "cancelled"} {
			jobs, err := repo.ArchiveJob.List(ctx, models.ArchiveJobEnrichPost, state, 0, 100)
			require.NoError(t, err)
			require.Empty(t, jobs)
		}
		return nil
	}))
	for _, query := range []string{"", "collection_revision=bad", "collection_revision=2&limit=101", "collection_revision=2&after_target=" + targets[0].UUID,
		"collection_revision=2&after_collection_revision=1", "collection_revision=2&after_collection_revision=2&after_target=" + targets[0].UUID} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v3/archive/collections/"+collection.UUID+"/enrichment-rebind-candidates?"+query, nil))
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	for _, body := range []string{`{"uuid":"bad"}`, `{"uuid":"bad","uuid":"bad"}`, strings.Repeat(" ", 65537)} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v3/archive/enrichment-rebindings/preview", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
}
