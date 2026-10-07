package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestArchiveReviewQueueHTTPPopulatedContracts(t *testing.T) {
	_, repo, input := nativeMetadataReviewFixture(t)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if _, err := repo.SourceAccount.Create(ctx, "native:reddit", "Review account"); err != nil {
			return err
		}
		post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "review-queue"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","id":"review-queue","title":"Retained post"}`))
		if err != nil {
			return err
		}
		title := "Retained post"
		_, err = repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Metadata: models.SourcePostMetadata{Title: &title}, Payload: *payload})
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: input.EntityUUID, Basis: "legacy", Details: []byte(`{}`)})
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	fixtures := map[string]json.RawMessage{}
	for _, kind := range []string{"accounts", "media", "metadata"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/review-queue/"+kind, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var page models.ArchiveReviewPage
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Equal(t, kind, page.Kind)
		require.Len(t, page.Items, 1)
		require.Equal(t, 1, page.Checked)
		require.Empty(t, page.Next)
		for _, private := range []string{`"payload"`, `"settings"`, `"request_json"`} {
			require.NotContains(t, w.Body.String(), private)
		}
		fixtures[kind] = append(json.RawMessage(nil), w.Body.Bytes()...)
	}
	// Opt-in fixture export lets the browser client validate actual HTTP bytes.
	// Normal test runs never rewrite checked-in fixtures.
	if path := os.Getenv("STASH_REVIEW_QUEUE_FIXTURE"); path != "" {
		encoded, err := json.MarshalIndent(fixtures, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0600))
	}
}

func TestArchiveReviewQueueHTTPReadOnlyAndStrictFilters(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "queue.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	handler := (&nativeArchiveRoutes{repo: db.Repository()}).router()
	for _, kind := range []string{"accounts", "media", "metadata"} {
		for _, tc := range []struct {
			method, suffix string
			status         int
		}{
			{http.MethodGet, "", 200}, {http.MethodGet, "?limit=1", 200},
			{http.MethodGet, "?after=" + uuid.NewString(), 200},
			{http.MethodGet, "?after=bad", 400}, {http.MethodGet, "?after=", 400},
			{http.MethodGet, "?limit=0", 400}, {http.MethodGet, "?limit=51", 400},
			{http.MethodGet, "?limit=one", 400}, {http.MethodGet, "?limit=1&limit=2", 400},
			{http.MethodGet, "?apply=true", 400}, {http.MethodPost, "", 405},
		} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, "/review-queue/"+kind+tc.suffix, nil))
			require.Equal(t, tc.status, w.Code, "%s %s: %s", tc.method, tc.suffix, w.Body.String())
			switch tc.status {
			case 200:
				require.JSONEq(t, `{"kind":"`+kind+`","items":[],"checked":0}`, w.Body.String())
			case 400:
				require.Contains(t, w.Body.String(), "invalid_review_queue_request")
			}
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/review-queue/unknown", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
}
