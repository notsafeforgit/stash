package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestCatalogAssociationHTTPBackfillMakesImportedPublisherVisible(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	var account *models.SourceAccount
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "imported"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("legacy-nfo", "reddit", []byte(`{"title":"Imported title"}`))
		if err != nil {
			return err
		}
		_, err = repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "legacy-nfo", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		if err != nil {
			return err
		}
		account, err = repo.SourceAccount.Create(ctx, "native:reddit", "CuteLilAsya")
		if err != nil {
			return err
		}
		_, err = repo.SourcePostLinks.ClaimAccount(ctx, models.SourcePostAccountClaimInput{
			SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "migration", Basis: "catalog-posts", ObservedAt: time.Now().UTC(), Details: []byte(`{}`)}, AccountUUID: account.UUID})
		return err
	}))
	var ids []string
	require.NoError(t, json.Unmarshal(get("/catalog-associations/posts?limit=1", 200).Body.Bytes(), &ids))
	require.Equal(t, []string{post.UUID}, ids)
	require.JSONEq(t, "[]", get("/catalog-associations/posts?after="+post.UUID, 200).Body.String())
	var preview models.CatalogAssociationPreview
	require.NoError(t, json.Unmarshal(get("/posts/"+post.UUID+"/catalog-association-preview", 200).Body.Bytes(), &preview))
	require.Equal(t, "link", preview.Action)
	require.JSONEq(t, "[]", get("/posts/"+post.UUID+"/publishers", 200).Body.String())
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	body := `{"post_uuids":["` + post.UUID + `"],"link_owners":true}`
	for _, expected := range []string{"linked", "preserved"} {
		r := httptest.NewRequest(http.MethodPost, "/catalog-associations/backfill", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, 200, w.Code, w.Body.String())
		var result []models.CatalogAssociationResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Len(t, result, 1)
		require.Equal(t, expected, result[0].Action)
	}
	var publishers []models.AccountReviewState
	require.NoError(t, json.Unmarshal(get("/posts/"+post.UUID+"/publishers", 200).Body.Bytes(), &publishers))
	require.Len(t, publishers, 1)
	require.Equal(t, account.UUID, publishers[0].UUID)
	for _, invalid := range []string{`{}`, `{"post_uuids":["bad"]}`, `{"post_uuids":["` + post.UUID + `","` + post.UUID + `"]}`} {
		r := httptest.NewRequest(http.MethodPost, "/catalog-associations/backfill", strings.NewReader(invalid))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, 400, w.Code)
	}
	get("/catalog-associations/posts?limit=101", 400)
	get("/posts/not-a-uuid/catalog-association-preview", 400)
}
