package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSourceRunHTTPScopedOwnershipAndLateEvidence(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "runs.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	routes := &ingestRoutes{service: service}
	var collection *models.SourceCollection
	var producer, other *models.IngestProducer
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		collection, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Source", Kind: "feed", Namespace: "native:reddit", State: "active", TargetURL: "https://www.reddit.com/user/example/submitted/"}})
		if err != nil {
			return err
		}
		producer, err = service.Repo.Ingest.CreateProducer(ctx, "Host")
		if err != nil {
			return err
		}
		other, err = service.Repo.Ingest.CreateProducer(ctx, "n8n")
		return err
	}))
	scopes := []models.IngestScope{{CollectionUUID: collection.UUID}}
	credential, token, err := service.IssueCredential(t.Context(), producer.UUID, scopes, nil)
	require.NoError(t, err)
	_, otherToken, err := service.IssueCredential(t.Context(), other.UUID, scopes, nil)
	require.NoError(t, err)
	handler := withIngestRoutes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }), service, false)
	request := func(h http.Handler, method, target, auth string, body any) *httptest.ResponseRecorder {
		var encoded []byte
		if body != nil {
			var err error
			encoded, err = json.Marshal(body)
			require.NoError(t, err)
		}
		r := httptest.NewRequest(method, target, bytes.NewReader(encoded))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	input := models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, Operation: "enrich", PolicySHA256: strings.Repeat("a", 64), Window: models.SourceWindow{Until: time.Now().UTC().Truncate(time.Millisecond)}}
	require.Equal(t, http.StatusUnauthorized, request(handler, http.MethodPost, ingestPath+"/runs", "", input).Code)
	require.Equal(t, http.StatusBadRequest, request(handler, http.MethodPost, ingestPath+"/runs", token, map[string]any{"command": "gallery-dl arbitrary-command"}).Code)
	w := request(handler, http.MethodPost, ingestPath+"/runs", token, input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var run models.SourceRun
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &run))
	require.Equal(t, "queued", run.State)
	path := ingestPath + "/runs/" + run.UUID
	owner := uuid.NewString()
	claim := map[string]any{"owner_uuid": owner, "policy_sha256": input.PolicySHA256, "lease_seconds": 60}
	w = request(handler, http.MethodPost, path+"/claim", token, claim)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &run))
	require.Equal(t, "running", run.State)
	require.Equal(t, http.StatusNoContent, request(handler, http.MethodPost, path+"/claim", otherToken, claim).Code)
	lease := map[string]any{"owner_uuid": owner, "fence": run.Fence, "progress": models.SourceRunProgress{ItemsSeen: 2, Cursor: "post_2"}}
	require.Equal(t, http.StatusConflict, request(handler, http.MethodPost, path+"/lease", otherToken, lease).Code, "different producer cannot reuse another producer's fence")
	w = request(handler, http.MethodPost, path+"/lease", token, lease)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error { return service.Repo.Ingest.RevokeCredential(ctx, credential.UUID) }))
	require.Equal(t, http.StatusUnauthorized, request(handler, http.MethodPost, path+"/lease", token, lease).Code)
	_, token, err = service.IssueCredential(t.Context(), producer.UUID, scopes, nil)
	require.NoError(t, err)
	finish := map[string]any{"owner_uuid": owner, "fence": run.Fence, "outcome": models.SourceRunOutcome{State: "succeeded"}}
	w = request(handler, http.MethodPost, path+"/lease", token, finish)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &run))
	require.Equal(t, "succeeded", run.State)
	w = request(handler, http.MethodPost, path+"/attempts", token, map[string]int{"after": 0})
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"outcome":"succeeded"`)
	w = request(handler, http.MethodPost, ingestPath+"/runs/list", token, map[string]any{"collection_uuid": collection.UUID})
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), run.UUID)
	// The run outcome fences execution, not durable evidence that was already
	// captured and is being delivered from a producer outbox after completion.
	event := ingest.CaptureEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: run.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, Kind: "source.capture", ObservedAt: time.Now().UTC(), ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "post1"}, Source: json.RawMessage(`{"category":"reddit","id":"post1","title":"Source post","author":"example"}`)}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	_, err = service.Capture(t.Context(), token, raw, ingest.Digest(raw))
	require.NoError(t, err)
	w = request(handler, http.MethodGet, path, token, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"state":"succeeded"`)
	// Application-only review routes never become part of the producer router.
	require.Equal(t, http.StatusNotFound, request(handler, http.MethodPost, path+"/review", token, map[string]any{"action": "retry", "expected_revision": run.Revision}).Code)
	input.RequestUUID = uuid.NewString()
	w = request(handler, http.MethodPost, ingestPath+"/runs", token, input)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &run))
	admin := chi.NewRouter()
	admin.Mount("/api/v3/ingest-admin", routes.adminRouter())
	w = request(admin, http.MethodPost, "/api/v3/ingest-admin/runs/"+run.UUID+"/review", "", map[string]any{"action": "cancel", "expected_revision": run.Revision})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"state":"cancelled"`)
}
