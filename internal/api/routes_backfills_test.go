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

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeBackfillHTTPImportAuthorityAndVerifiedCompletion(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "backfills.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	service := ingest.New(repo)
	var root *models.MediaRoot
	var collection *models.SourceCollection
	var producer *models.IngestProducer
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Backfill", State: "active", Binding: binding}})
		require.NoError(t, err)
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Twitter", Kind: "feed", Namespace: "native:twitter", State: "active", RootUUID: &root.UUID, PathPrefix: "Account", TargetURL: "https://x.com/i/user/123"}})
		require.NoError(t, err)
		producer, err = repo.Ingest.CreateProducer(ctx, "n8n")
		return err
	}))
	credential, token, err := service.IssueCredential(t.Context(), producer.UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	_, named, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	admin := (&nativeArchiveRoutes{repo: repo}).router()
	worker := (&ingestRoutes{service: service}).router()
	request := func(handler http.Handler, method, path, auth string, value any) *httptest.ResponseRecorder {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	source := uuid.NewString()
	legacy := func(account, mode string) models.LegacyBackfillRecord {
		body, err := json.Marshal(map[string]string{"platform": "reddit", "account": account, "component": mode,
			"completed_at": "2026-09-29T00:00:00Z", "result_json": `{"command_failed":false,"exit_code":0,"network_blocked":false,"stdout_tail":"private log","exhaustive_history_verified":false,"user_accepted_as_complete":true}`})
		require.NoError(t, err)
		return models.LegacyBackfillRecord{RootUUID: root.UUID, SourceUUID: source, Table: "backfill_completion", Record: body}
	}
	records := map[string]any{"records": []models.LegacyBackfillRecord{legacy("Example", "reddit-new"), legacy("Example", "reddit-top")}}
	w := request(admin, "POST", "/backfills/import", "", records)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var imported []models.BackfillDecisionSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &imported))
	require.Len(t, imported, 2)
	require.NotContains(t, w.Body.String(), "private log")
	require.Equal(t, w.Body.String(), request(admin, "POST", "/backfills/import", "", records).Body.String())
	w = request(admin, "GET", "/backfills/"+imported[0].UUID, "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "private log")
	stateInput := backfillStatusInput{BackfillSubject: models.BackfillSubject{RootUUID: root.UUID, Platform: "reddit", Account: "EXAMPLE"}, Component: "reddit-profile-new"}
	w = request(worker, "POST", ingestPath+"/backfills/status", token, stateInput)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var status models.BackfillStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.Equal(t, "completed", status.State)
	require.True(t, status.AccountComplete)
	require.NotContains(t, w.Body.String(), "private log")
	require.Equal(t, http.StatusForbidden, request(worker, "POST", ingestPath+"/backfills/status", named, stateInput).Code)
	require.Equal(t, http.StatusUnauthorized, request(worker, "POST", ingestPath+"/backfills/status", "", stateInput).Code)
	require.Equal(t, http.StatusNotFound, request(worker, "POST", ingestPath+"/backfills/import", token, records).Code)
	otherRoot := stateInput
	otherRoot.RootUUID = uuid.NewString()
	require.Equal(t, http.StatusForbidden, request(worker, "POST", ingestPath+"/backfills/status", token, otherRoot).Code)
	invalid := legacy("another", "unknown")
	w = request(admin, "POST", "/backfills/import", "", map[string]any{"records": []models.LegacyBackfillRecord{legacy("another", "reddit-new"), invalid}})
	require.Equal(t, http.StatusBadRequest, w.Code)
	stateInput.Account, stateInput.Component = "another", "reddit-new"
	w = request(worker, "POST", ingestPath+"/backfills/status", token, stateInput)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.Equal(t, "needed", status.State, "an invalid later batch item rolls back the first one")
	csrf := httptest.NewRequest("POST", "/backfills/import", strings.NewReader(`{}`))
	csrf.Header.Set("Origin", "https://another.invalid")
	w = httptest.NewRecorder()
	admin.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)

	until := time.Now().UTC().Truncate(time.Millisecond)
	input := models.SourceRunRequest{RequestUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		Operation: "download", PolicySHA256: strings.Repeat("a", 64), Window: models.SourceWindow{Until: until}}
	w = request(worker, "POST", ingestPath+"/runs", token, input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var run models.SourceRun
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &run))
	completion := models.BackfillCompletion{UUID: uuid.NewString(), BackfillSubject: models.BackfillSubject{RootUUID: root.UUID, Platform: "twitter", Account: "123"},
		Component: "twitter", Window: input.Window, PolicySHA256: input.PolicySHA256, Requests: []models.SourceRunRequest{input}}
	w = request(worker, "POST", ingestPath+"/backfills/complete", token, completion)
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "backfill_incomplete")
	w = request(worker, "POST", ingestPath+"/runs/"+run.UUID+"/claim", token, map[string]any{"owner_uuid": uuid.NewString(), "policy_sha256": input.PolicySHA256, "lease_seconds": 60})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &run))
	w = request(worker, "POST", ingestPath+"/runs/"+run.UUID+"/lease", token, map[string]any{"owner_uuid": run.OwnerUUID, "fence": run.Fence, "outcome": models.SourceRunOutcome{State: "succeeded"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = request(worker, "POST", ingestPath+"/backfills/complete", token, completion)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var finished models.BackfillDecisionSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &finished))
	require.Equal(t, "source_runs", finished.Basis)
	require.Equal(t, w.Body.String(), request(worker, "POST", ingestPath+"/backfills/complete", token, completion).Body.String())
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Ingest.RevokeCredential(ctx, credential.UUID) }))
	require.Equal(t, http.StatusUnauthorized, request(worker, "POST", ingestPath+"/backfills/complete", token, completion).Code)
}
