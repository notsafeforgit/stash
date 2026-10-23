package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestProducerFileHTTPRetryRecoveryAndBoundaries(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "retry.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	dir := t.TempDir()
	content := []byte("queued bytes")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample.mp4"), content, 0600))
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	repo := db.Repository()
	service := ingest.New(repo)
	var root *models.MediaRoot
	var collection *models.SourceCollection
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Files", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Files", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
		if err != nil {
			return err
		}
		producer, err = repo.Ingest.CreateProducer(ctx, "Fixture")
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	event := ingest.FileEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: root.UUID,
		Kind: "file.completed", ObservedAt: time.Now(), RelativePath: "sample.mp4", Size: int64(len(content)), SHA256: ingest.Digest(content), MediaKind: models.ArchiveScene}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	accepted, err := service.FileCompleted(t.Context(), token, raw, ingest.Digest(raw))
	require.NoError(t, err)
	var parent *models.ArchiveJob
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		parent, err = repo.ArchiveJob.Find(ctx, accepted.JobUUID)
		return err
	}))
	durable := job.NewDurable(repo)
	claimed, err := durable.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	parent, err = durable.Publish(t.Context(), claimed.Lease(), func(context.Context, *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		return models.ArchiveJobOutcome{State: "failed", ErrorCode: "file_changed", Result: json.RawMessage(`{"registration_committed":false,"media_ingested":false,"failure":{"stage":"registration","reason":"path_removed_or_moved"}}`)}, nil
	})
	require.NoError(t, err)
	routes := &nativeArchiveRoutes{repo: repo, fileIngestion: true}
	handler := routes.router()
	call := func(request string, revision int64, origin string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{"request_uuid": request, "expected_revision": revision})
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, "/file-verifications/"+parent.UUID+"/retry", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	id := uuid.NewString()
	require.Equal(t, http.StatusForbidden, call(id, parent.Revision, "https://foreign.invalid").Code)
	w := call(id, parent.Revision, "")
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var first ingest.FileRetryStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.Equal(t, parent.UUID, first.FromJobUUID)
	require.NotEqual(t, parent.UUID, first.JobUUID)
	require.NotContains(t, w.Body.String(), dir)
	for _, id := range []string{parent.UUID, first.JobUUID} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/activity/jobs/"+id, nil))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var detail activityJobDetail
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &detail))
		if id == parent.UUID {
			require.Equal(t, &ingest.FileFailure{Stage: "registration", Reason: "path_removed_or_moved"}, detail.FileFailure)
		} else {
			require.Equal(t, parent.UUID, detail.RetryOfJobUUID)
		}
		require.NotContains(t, response.Body.String(), dir)
		require.NotContains(t, response.Body.String(), "arguments")
	}
	routes.fileIngestion = false
	require.NoError(t, os.Remove(filepath.Join(dir, "sample.mp4")))
	w = call(id, parent.Revision, "")
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var recovered ingest.FileRetryStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, first, recovered)
	require.Equal(t, http.StatusConflict, call(id, parent.Revision+1, "").Code)
	require.Equal(t, http.StatusServiceUnavailable, call(uuid.NewString(), parent.Revision, "").Code)
	require.Equal(t, http.StatusBadRequest, call("123", parent.Revision, "").Code)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/file-verification-retries/"+id, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, first, recovered)
	w = httptest.NewRecorder()
	routes.routerWithSourceTracking(false).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/file-verification-retries/"+id, nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}
