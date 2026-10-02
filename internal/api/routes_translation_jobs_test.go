package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/translation"
	"github.com/stretchr/testify/require"
)

func TestTranslationExecutionRoutesAndWorkerGate(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "translation-execution.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	s := translation.New(repo)
	require.Nil(t, newTranslationWorkerRuntime(config.GetInstance(), s), "migration starts with provider execution disabled")
	config.GetInstance().SetBool(config.TranslationWorkerEnabled, true)
	config.GetInstance().SetString(config.TranslationShellPath, filepath.Join(t.TempDir(), "missing-trans"))
	require.Nil(t, newTranslationWorkerRuntime(config.GetInstance(), s), "missing provider configuration cannot start or fail queued work")
	config.GetInstance().SetBool(config.TranslationWorkerEnabled, false)
	var post *models.SourcePost
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "translation-execution"}, "")
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo, translations: s}).router()
	request := func(method, path string, input any, expected int, output any) {
		t.Helper()
		body, err := json.Marshal(input)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		require.Equal(t, expected, w.Code, "%s %s: %s", method, path, w.Body.String())
		if output != nil {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), output))
		}
	}
	input := map[string]any{"request": models.TranslationRequestInput{OriginalText: "Original source", TargetLanguage: "en", Policy: models.TranslationBingTextV1}, "field": "caption", "schedule": models.TranslationTargetSchedule{State: "held", Priority: 25}}
	var target models.TranslationTarget
	request("POST", "/posts/"+post.UUID+"/translation-targets", input, 200, &target)
	base := "/translation-targets/" + target.UUID
	request("POST", "/translation-jobs/admit", struct{}{}, 200, nil)
	request("POST", base+"/retry", map[string]int{"expected_revision": target.Revision}, 200, &target)
	request("PUT", base+"/schedule", map[string]any{"expected_revision": 1, "schedule": models.TranslationTargetSchedule{State: "held"}}, 409, nil)
	var job models.ArchiveJob
	request("POST", "/translation-jobs/admit", struct{}{}, 202, &job)
	var binding models.TranslationJobTarget
	request("GET", base+"/job", nil, 200, &binding)
	require.Equal(t, job.UUID, binding.JobUUID)
	jobPath := "/translation-jobs/" + job.UUID
	request("GET", jobPath, nil, 200, nil)
	request("POST", jobPath+"/cancel", map[string]int64{"expected_revision": job.Revision}, 200, &job)
	require.Equal(t, "cancelled", job.State)
	request("GET", base, nil, 200, &target)
	require.Equal(t, "held", target.State)
	request("GET", base+"/job?revision=2", nil, 200, &binding)
	require.Equal(t, job.UUID, binding.JobUUID, "historical target revisions retain their original job")
	request("POST", base+"/retry", map[string]int{"expected_revision": target.Revision}, 200, &target)
	calls := 0
	worker := translation.NewWorker(s, translation.ProviderFunc(func(context.Context, models.TranslationRequest) (models.TranslationCacheInput, error) {
		calls++
		translated := "Translated source"
		return models.TranslationCacheInput{Status: "translated", TranslatedText: &translated}, nil
	}))
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, 1, calls)
	request("GET", base, nil, 200, &target)
	require.Equal(t, "completed", target.State)
	var jobs []models.ArchiveJob
	request("GET", "/translation-requests/"+target.RequestUUID+"/jobs", nil, 200, &jobs)
	require.Len(t, jobs, 2)
	require.Equal(t, "cancelled", jobs[0].State)
	require.Equal(t, "succeeded", jobs[1].State)
	var attempts []models.ArchiveJobAttempt
	request("GET", "/translation-jobs/"+jobs[1].UUID+"/attempts", nil, 200, &attempts)
	require.Len(t, attempts, 1)
	require.Equal(t, "succeeded", attempts[0].Outcome)
	request("POST", base+"/retry", map[string]int{"expected_revision": target.Revision}, 409, nil)
	for _, path := range []string{base + "/job?revision=0", base + "/job?revision=999", base + "/job?revision=no", jobPath + "/attempts?limit=101", "/translation-jobs/not-a-uuid", "/translation-requests/invalid/jobs"} {
		request("GET", path, nil, 400, nil)
	}
	request("GET", "/translation-jobs/"+uuid.NewString(), nil, 404, nil)
	request("POST", "/posts/"+uuid.NewString()+"/translation-targets", input, 404, nil)
	input["collection_uuid"], input["collection_revision"] = uuid.NewString(), 1
	request("POST", "/posts/"+post.UUID+"/translation-targets", input, 400, nil)
	request("POST", "/translation-jobs/admit", map[string]string{"command": "arbitrary"}, 400, nil)
	csrf := httptest.NewRequest("POST", "/translation-jobs/admit", bytes.NewBufferString(`{}`))
	csrf.Header.Set("Origin", "https://unrelated.invalid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)

	var producer *models.IngestProducer
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Scoped producer")
		if err != nil {
			return err
		}
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Feed", Kind: "feed", State: "active"}})
		return err
	}))
	intake := ingest.New(repo)
	_, token, err := intake.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
	require.NoError(t, err)
	producerHandler := withIngestRoutes(http.NotFoundHandler(), intake, false)
	for _, prefix := range []string{ingestPath, "/api/v3/archive"} {
		r := httptest.NewRequest("POST", prefix+"/translation-jobs/admit", bytes.NewBufferString(`{}`))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		producerHandler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}
