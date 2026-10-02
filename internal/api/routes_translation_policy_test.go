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

func TestTranslationPolicyRoutesRequireReviewedRevisionsAndExposeCaptureDecision(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "translation-policy.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var collection *models.SourceCollection
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Source", Kind: "feed", State: "active"}})
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
	path := "/collections/" + collection.UUID + "/translation-policy"
	require.JSONEq(t, "null", string(request("GET", path, nil, 200)))
	input := models.TranslationPolicyInput{ExpectedCollectionRevision: collection.Revision, Definition: models.TranslationPolicyDefinition{
		Enabled: true, ProviderPolicy: models.TranslationBingTextV1, TargetLanguage: "en", Title: true, Priority: 100}}
	var policy models.TranslationPolicy
	require.NoError(t, json.Unmarshal(request("PUT", path, input, 200), &policy))
	require.Equal(t, "review", policy.Origin)
	request("PUT", path, input, 409)
	input.ExpectedRevision = policy.Revision
	input.Definition.TargetLanguage = "invalid language"
	request("PUT", path, input, 400)
	var history []models.TranslationPolicy
	require.NoError(t, json.Unmarshal(request("GET", path+"/history?after=0", nil, 200), &history))
	require.Equal(t, []models.TranslationPolicy{policy}, history)
	request("GET", path+"/history?after=-1", nil, 400)
	request("GET", "/collections/invalid/translation-policy", nil, 400)
	request("PUT", path, map[string]any{"definition": map[string]any{"unexpected": true}}, 400)
	var decision *models.CaptureTranslationDecision
	captureID := uuid.NewString()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "policy-http"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","id":"policy-http"}`))
		if err != nil {
			return err
		}
		title := "Exact title"
		_, err = repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: captureID, PostUUID: post.UUID, Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Now(), RetentionPolicy: archive.SourceRetentionVersion, Metadata: models.SourcePostMetadata{Title: &title}, Payload: *payload})
		if err != nil {
			return err
		}
		scope := models.CollectionCapture{CaptureUUID: captureID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision}
		if err := repo.SourceCollection.RecordCapture(ctx, scope); err != nil {
			return err
		}
		decision, err = repo.TranslationPolicy.ScheduleCapture(ctx, scope, time.Now())
		return err
	}))
	decisionPath := "/captures/" + captureID + "/translation-decision?collection_uuid=" + collection.UUID + "&collection_revision=1"
	var inspected models.CaptureTranslationDecision
	require.NoError(t, json.Unmarshal(request("GET", decisionPath, nil, 200), &inspected))
	require.Equal(t, decision, &inspected)
	request("GET", "/captures/"+captureID+"/translation-decision", nil, 400)
	request("GET", "/captures/"+uuid.NewString()+"/translation-decision?collection_uuid="+collection.UUID+"&collection_revision=1", nil, 404)
	csrf := httptest.NewRequest("PUT", path, bytes.NewBufferString(`{}`))
	csrf.Header.Set("Origin", "https://unrelated.invalid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)
	intake := ingest.New(repo)
	_, token, err := intake.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	producerHandler := withIngestRoutes(http.NotFoundHandler(), intake, false)
	for _, target := range []string{path, path + "/history", decisionPath} {
		for _, method := range []string{"GET", "PUT"} {
			r := httptest.NewRequest(method, "/api/v3/archive"+target, bytes.NewBufferString(`{}`))
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			producerHandler.ServeHTTP(w, r)
			require.Equal(t, http.StatusNotFound, w.Code)
		}
	}
}
