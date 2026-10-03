package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentRoutesRequireReviewedRevisionsAndApplicationAccess(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "enrichment.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var post *models.SourcePost
	var url *models.SourcePostURLObservation
	var collection *models.SourceCollection
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
		if err != nil {
			return err
		}
		url, err = repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: models.SourcePostEvidence{
			UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "review", Basis: "source_post", ObservedAt: time.Now()}, URL: "https://reddit.com/comments/abc123"})
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Source feed", Kind: "feed", State: "active"}})
		if err != nil {
			return err
		}
		producer, err = repo.Ingest.CreateProducer(ctx, "Source producer")
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, path string, input any, status int) []byte {
		t.Helper()
		body, err := json.Marshal(input)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.Bytes()
	}
	postPath := "/posts/" + post.UUID + "/enrichment-targets"
	input := map[string]any{"url_uuid": url.URLUUID, "collection_uuid": collection.UUID, "collection_revision": collection.Revision,
		"policy": models.EnrichmentGalleryMetadataV1, "schedule": models.EnrichmentSchedule{State: "held", Reason: "owner_review", Priority: 20}}
	var held models.EnrichmentTarget
	require.NoError(t, json.Unmarshal(request("POST", postPath, input, 200), &held))
	require.Equal(t, "review", held.Origin)
	require.Equal(t, url.URL, held.URL)
	input["schedule"] = models.EnrichmentSchedule{State: "pending", Priority: 100}
	var replay models.EnrichmentTarget
	require.NoError(t, json.Unmarshal(request("POST", postPath, input, 200), &replay))
	require.Equal(t, held, replay)
	path := "/enrichment-targets/" + held.UUID
	request("GET", path, nil, 200)
	var page []models.EnrichmentTarget
	require.NoError(t, json.Unmarshal(request("GET", postPath+"?state=held&limit=1", nil, 200), &page))
	require.Equal(t, []models.EnrichmentTarget{held}, page)
	require.JSONEq(t, "[]", string(request("GET", postPath+"?after="+held.UUID, nil, 200)))
	request("GET", "/collections/"+collection.UUID+"/enrichment-targets", nil, 200)
	request("GET", "/collections/"+uuid.NewString()+"/enrichment-targets", nil, 404)
	request("GET", postPath+"?limit=101", nil, 400)
	request("GET", postPath+"?after=invalid", nil, 400)
	request("GET", postPath+"?state=arbitrary", nil, 400)
	request("GET", path+"/history?after=-1", nil, 400)
	request("GET", "/enrichment-targets/invalid", nil, 400)
	request("GET", "/enrichment-targets/"+uuid.NewString(), nil, 404)
	request("POST", "/posts/"+uuid.NewString()+"/enrichment-targets", input, 404)
	input["origin"] = "migration"
	request("POST", postPath, input, 400)
	delete(input, "origin")
	change := map[string]any{"expected_revision": held.Revision, "schedule": models.EnrichmentSchedule{State: "pending", Priority: held.Priority, NotBefore: held.NotBefore}}
	var pending models.EnrichmentTarget
	require.NoError(t, json.Unmarshal(request("PUT", path+"/schedule", change, 200), &pending))
	require.Equal(t, held.Revision+1, pending.Revision)
	request("PUT", path+"/schedule", change, 409)
	change["expected_revision"] = pending.Revision
	change["schedule"] = models.EnrichmentSchedule{State: "completed"}
	request("PUT", path+"/schedule", change, 400)
	request("POST", path+"/complete", map[string]any{}, 404)
	var completion *models.EnrichmentCompletion
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","id":"abc123","title":"Source title"}`))
		if err != nil {
			return err
		}
		captureID := uuid.NewString()
		_, err = repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: captureID, PostUUID: post.UUID, Origin: "gallery-dl", Platform: "reddit",
			CapturedAt: time.Now(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		if err != nil {
			return err
		}
		if err := repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CaptureUUID: captureID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision}); err != nil {
			return err
		}
		completion, err = repo.EnrichmentWork.Complete(ctx, models.EnrichmentCompletionInput{UUID: uuid.NewString(), TargetUUID: pending.UUID, ExpectedRevision: pending.Revision, CaptureUUIDs: []string{captureID}}, time.Now())
		return err
	}))
	completionPath := "/enrichment-completions/" + completion.UUID
	var inspected models.EnrichmentCompletion
	require.NoError(t, json.Unmarshal(request("GET", completionPath, nil, 200), &inspected))
	require.Equal(t, completion, &inspected)
	request("GET", "/enrichment-completions/"+uuid.NewString(), nil, 404)
	var history []models.EnrichmentTargetHistory
	require.NoError(t, json.Unmarshal(request("GET", path+"/history?after=1&limit=2", nil, 200), &history))
	require.Len(t, history, 2)
	require.Equal(t, []string{"pending", "completed"}, []string{history[0].State, history[1].State})
	csrf := httptest.NewRequest("PUT", path+"/schedule", bytes.NewBufferString(`{}`))
	csrf.Header.Set("Origin", "https://unrelated.invalid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)
	intake := ingest.New(repo)
	_, token, err := intake.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	producerHandler := withIngestRoutes(http.NotFoundHandler(), intake, false)
	for _, target := range []string{postPath, path, path + "/history", path + "/schedule", completionPath, "/collections/" + collection.UUID + "/enrichment-targets"} {
		for _, method := range []string{"GET", "POST", "PUT"} {
			r := httptest.NewRequest(method, "/api/v3/archive"+target, bytes.NewBufferString(`{}`))
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			producerHandler.ServeHTTP(w, r)
			require.Equal(t, http.StatusNotFound, w.Code)
		}
	}
}
