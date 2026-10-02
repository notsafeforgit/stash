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

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/translation"
	"github.com/stretchr/testify/require"
)

func TestTranslationActivationRoutesPreviewApplyReplayAndAuthorization(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "activation.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	s := translation.New(repo)
	var target *models.TranslationTarget
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "activation-routes"}, "")
		if err != nil {
			return err
		}
		request, err := repo.TranslationWork.RetainRequest(ctx, models.TranslationRequestInput{OriginalText: "Held source text", TargetLanguage: "en", Policy: models.TranslationBingTextV1})
		if err != nil {
			return err
		}
		target, err = repo.TranslationWork.RetainTarget(ctx, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID, Field: "title", Origin: "review"}, models.TranslationTargetSchedule{State: "held", Priority: 25}, s.Durable.Now())
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
	input := models.TranslationActivationInput{UUID: uuid.NewString(), Targets: []models.TranslationTargetRef{{TargetUUID: target.UUID, Revision: target.Revision}}}
	var plan models.TranslationActivationPlan
	request("POST", "/translation-activations/preview", input, 200, &plan)
	apply := map[string]any{"input": input, "expected_plan_sha256": plan.PlanSHA256}
	request("GET", "/translation-activations/"+input.UUID, nil, 404, nil)
	var result models.TranslationActivation
	request("POST", "/translation-activations", apply, 200, &result)
	require.Equal(t, plan, result.TranslationActivationPlan)
	var same models.TranslationActivation
	request("POST", "/translation-activations", apply, 200, &same)
	require.Equal(t, result, same)
	request("GET", "/translation-activations/"+input.UUID, nil, 200, &same)
	require.Equal(t, result, same)
	request("POST", "/translation-activations/preview", input, 409, nil)
	request("POST", "/translation-activations", map[string]any{"input": input, "expected_plan_sha256": strings.Repeat("0", 64)}, 409, nil)
	request("POST", "/translation-activations", map[string]any{"input": input}, 400, nil)
	request("POST", "/translation-activations/preview", map[string]any{"command": "unexpected"}, 400, nil)
	request("GET", "/translation-activations/invalid", nil, 400, nil)
	request("POST", "/translation-activations/preview", models.TranslationActivationInput{UUID: uuid.NewString(), Targets: []models.TranslationTargetRef{input.Targets[0], input.Targets[0]}}, 400, nil)
	base := "/automation-snapshots/" + uuid.NewString() + "/translation-import/held-targets"
	request("GET", base, nil, 400, nil)
	request("GET", base+"?expected_manifest_sha256="+strings.Repeat("a", 64), nil, 409, nil)
	for _, query := range []string{"after=-1", "after=no", "limit=0", "limit=101", "limit=no"} {
		request("GET", base+"?expected_manifest_sha256="+strings.Repeat("a", 64)+"&"+query, nil, 400, nil)
	}
	for _, path := range []string{"/translation-activations", "/translation-activations/preview"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://unrelated.invalid")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusForbidden, w.Code)
	}
	var producer *models.IngestProducer
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Activation cannot be producer controlled")
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
	for _, path := range []string{"/translation-activations", "/translation-activations/preview", "/translation-activations/" + input.UUID, base} {
		for _, method := range []string{"GET", "POST"} {
			r := httptest.NewRequest(method, "/api/v3/archive"+path, strings.NewReader(`{}`))
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			producerHandler.ServeHTTP(w, r)
			require.Equal(t, http.StatusNotFound, w.Code)
		}
	}
}
