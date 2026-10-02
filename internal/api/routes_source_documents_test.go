package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSourceDocumentHTTPInspectionAndRevisionCheckedSelection(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "documents.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var collection *models.SourceCollection
	var post *models.SourcePost
	var doc, empty *models.SourceDocument
	var source, folder *models.SourceDocumentSource
	var claim *models.SourceDocumentHeadClaim
	content := []byte("<html>Original bytes\xff</html>")
	const path = "folder\\literal/a + b.nfo"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		collection, err = repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Imported", Kind: "directory", State: "disabled"}})
		require.NoError(t, err)
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "document-http"}, "")
		require.NoError(t, err)
		doc, err = repo.SourceDocument.Retain(ctx, models.SourceDocumentInput{Content: content, Encoding: "unknown", Parser: "legacy-catalog-nfo-v1", ParseStatus: "invalid", Warnings: json.RawMessage(`["Invalid XML"]`), Parsed: json.RawMessage(`{"unknown_field":["preserved"]}`)})
		require.NoError(t, err)
		empty, err = repo.SourceDocument.Retain(ctx, models.SourceDocumentInput{Parser: "legacy-catalog-nfo-v1", ParseStatus: "empty", Warnings: json.RawMessage(`[]`), Parsed: json.RawMessage(`{}`)})
		require.NoError(t, err)
		source, err = repo.SourceDocument.RecordSource(ctx, models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
			RelativePath: path, PostUUID: &post.UUID, CapturedAt: "2026-09-29T01:02:03.123456-07:00", Origin: "migration"})
		require.NoError(t, err)
		folder, err = repo.SourceDocument.RecordSource(ctx, models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
			RelativePath: "folder/default.nfo", Origin: "migration"})
		require.NoError(t, err)
		claim, err = repo.SourceDocument.RecordHeadClaim(ctx, models.SourceDocumentHeadClaim{UUID: uuid.NewString(), SourceUUID: source.UUID, CollectionUUID: collection.UUID, RelativePath: path, Origin: "migration"})
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(method, target string, input any) *httptest.ResponseRecorder {
		body, err := json.Marshal(input)
		require.NoError(t, err)
		r := httptest.NewRequest(method, target, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	get := func(target string) *httptest.ResponseRecorder {
		w := request(http.MethodGet, target, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w
	}
	w := get("/documents/" + doc.UUID)
	var found models.SourceDocument
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &found))
	require.Equal(t, *doc, found)
	require.NotContains(t, w.Body.String(), "Original bytes")
	w = get("/documents/" + doc.UUID + "/content")
	require.Equal(t, content, w.Body.Bytes())
	require.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	require.Contains(t, w.Header().Get("Content-Disposition"), `attachment; filename="source-document-`+doc.UUID)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	w = get("/documents/" + empty.UUID + "/content")
	require.Empty(t, w.Body.Bytes())
	require.Equal(t, "0", w.Header().Get("Content-Length"))
	w = get("/document-sources/" + source.UUID)
	var foundSource models.SourceDocumentSource
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &foundSource))
	require.Equal(t, *source, foundSource)
	w = get("/document-claims/" + claim.UUID)
	var foundClaim models.SourceDocumentHeadClaim
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &foundClaim))
	require.Equal(t, *claim, foundClaim)
	postURL := "/posts/" + post.UUID + "/documents"
	w = get(postURL + "?limit=1")
	var sources []models.SourceDocumentSource
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sources))
	require.Equal(t, []models.SourceDocumentSource{*source}, sources)
	require.NotContains(t, w.Body.String(), `"parsed"`)
	require.NotContains(t, w.Body.String(), `"warnings"`)
	require.JSONEq(t, `[]`, get(postURL+"?after="+source.UUID).Body.String())
	base := "/collections/" + collection.UUID
	query := "?path=" + url.QueryEscape(path)
	w = get(base + "/documents?path=" + url.QueryEscape(folder.RelativePath))
	sources = nil
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sources))
	require.Equal(t, []models.SourceDocumentSource{*folder}, sources, "unattributed folder documents remain queryable")
	headURL := base + "/document-head"
	require.JSONEq(t, `null`, get(headURL+query).Body.String())
	put := map[string]any{"relative_path": path, "expected_revision": 0, "state": "linked", "source_uuid": source.UUID, "claim_uuid": claim.UUID, "reason": "Reviewed selection"}
	w = request(http.MethodPut, headURL, put)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var selected models.SourceDocumentHead
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &selected))
	require.Equal(t, "review", selected.Origin)
	require.Equal(t, 1, selected.Revision)
	require.Equal(t, &source.UUID, selected.SourceUUID)
	require.Equal(t, &claim.UUID, selected.ClaimUUID)
	require.JSONEq(t, w.Body.String(), get(headURL+query).Body.String())
	require.Equal(t, http.StatusConflict, request(http.MethodPut, headURL, put).Code, "lost replies cannot apply another decision")
	put["expected_revision"], put["source_uuid"] = 1, folder.UUID
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, headURL, put).Code, "a different path cannot be selected")
	put["source_uuid"] = source.UUID
	put["origin"] = "capture"
	require.Equal(t, http.StatusBadRequest, request(http.MethodPut, headURL, put).Code, "applications cannot fabricate a source observation")
	delete(put, "origin")
	put["state"], put["source_uuid"], put["claim_uuid"] = "unlinked", "", ""
	w = request(http.MethodPut, headURL, put)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var unlinked models.SourceDocumentHead
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &unlinked))
	require.Equal(t, 2, unlinked.Revision)
	require.Nil(t, unlinked.SourceUUID)
	claimsURL := headURL + "/claims" + query
	w = get(claimsURL)
	var claims []models.SourceDocumentHeadClaim
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &claims))
	require.Equal(t, []models.SourceDocumentHeadClaim{*claim}, claims)
	require.JSONEq(t, `[]`, get(claimsURL+"&after="+claim.UUID).Body.String())
	historyURL := headURL + "/history" + query
	var history []models.SourceDocumentHead
	require.NoError(t, json.Unmarshal(get(historyURL+"&limit=1").Body.Bytes(), &history))
	require.Equal(t, []models.SourceDocumentHead{selected}, history)
	history = nil
	require.NoError(t, json.Unmarshal(get(historyURL+"&after=1").Body.Bytes(), &history))
	require.Equal(t, []models.SourceDocumentHead{unlinked}, history)
	for _, target := range []string{postURL + "?after=bad", postURL + "?limit=101", postURL + "?limit=0", postURL + "?limit=1.5", base + "/documents", headURL + "?path=%00", historyURL + "&after=-1", historyURL + "&after=no", claimsURL + "&after=no", "/documents/no", "/document-sources/no", "/document-claims/no"} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodGet, target, nil).Code, target)
	}
	for _, target := range []string{"/documents/" + uuid.NewString(), "/documents/" + uuid.NewString() + "/content", "/document-sources/" + uuid.NewString(), "/document-claims/" + uuid.NewString(), "/posts/" + uuid.NewString() + "/documents", "/collections/" + uuid.NewString() + "/documents" + query} {
		require.Equal(t, http.StatusNotFound, request(http.MethodGet, target, nil).Code, target)
	}
	r := httptest.NewRequest(http.MethodPut, headURL, bytes.NewReader([]byte(`{}`)))
	r.Header.Set("Origin", "https://another.example")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	// Router calls obtain a new managed transaction after reopening the database.
	require.JSONEq(t, `[]`, get(historyURL+"&after=2").Body.String())
	selected = models.SourceDocumentHead{}
	require.NoError(t, json.Unmarshal(get(headURL+query).Body.Bytes(), &selected))
	require.Equal(t, unlinked, selected)

	// Producer routes expose no retained document or selection operation.
	producer := withIngestRoutes(http.NotFoundHandler(), ingest.New(repo), false)
	for _, target := range []string{"/api/v3/archive/documents/" + doc.UUID, ingestPath + "/documents/" + doc.UUID} {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		r.Header.Set("Authorization", "Bearer producer-token")
		w := httptest.NewRecorder()
		producer.ServeHTTP(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}
