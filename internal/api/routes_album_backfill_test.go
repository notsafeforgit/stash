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
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func albumHTTPPost(t *testing.T, repo models.Repository) *models.SourcePost {
	t.Helper()
	var post *models.SourcePost
	retained, err := archive.RetainSourcePayload([]byte(`{"category":"reddit","id":"fixture","title":"Album title"}`))
	require.NoError(t, err)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", retained)
	require.NoError(t, err)
	title, date := "Album title", "2026-09-28"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "fixture"}, "")
		if err != nil {
			return err
		}
		capture, err := repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload, Metadata: models.SourcePostMetadata{Title: &title, PublishedAt: &date}})
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.RecordManifest(ctx, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, DeclaredAlbum: true, Complete: true, Entries: []models.SourceAttachmentEntry{
			{Position: 0, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "first"}, MediaKind: "image"},
			{Position: 1, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "second"}, MediaKind: "video"},
		}})
		if err != nil {
			return err
		}
		post, err = repo.SourceEvidence.FindPost(ctx, post.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: post.Revision, Mode: "pinned", CaptureUUID: capture.UUID, Origin: "review"})
		return err
	}))
	return post
}

func TestAlbumBackfillHTTPPreviewAdmissionRetryAndRuntime(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "albums.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	post := albumHTTPPost(t, repo)
	s := gallery.NewAlbumBackfill(repo)
	handler := (&nativeArchiveRoutes{repo: repo, albums: s}).router()
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	base := "/posts/" + post.UUID
	w := request(http.MethodGet, "/album-backfill-posts?limit=1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var selected []models.SelectedSourcePost
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &selected))
	require.Len(t, selected, 1)
	require.Equal(t, post.UUID, selected[0].PostUUID)
	require.Equal(t, "active", selected[0].PostState)
	require.Equal(t, "pinned", selected[0].Mode)
	require.NotEmpty(t, selected[0].SelectionUUID)
	w = request(http.MethodGet, "/album-backfill-posts?after="+post.UUID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `[]`, w.Body.String())
	for _, query := range []string{"limit=101", "after=no", "after=-1", "limit=0", "limit=1.2"} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodGet, "/album-backfill-posts?"+query, nil).Code)
	}
	previewInput := map[string]string{"policy": models.SourceAlbumIdentifiersV1}
	w = request(http.MethodPost, base+"/album-backfill/preview", previewInput)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var preview gallery.AlbumPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.Equal(t, "create", preview.Action)
	require.Equal(t, "2026-09-28", *preview.InitialMetadata.Date)
	require.Len(t, preview.Entries, 2)
	require.Len(t, preview.Matches, 2)
	require.NotContains(t, w.Body.String(), `"Namespace"`)
	input := map[string]string{"request_uuid": uuid.NewString(), "policy": preview.Policy, "signature": preview.Signature}
	require.Equal(t, http.StatusNotFound, request(http.MethodPost, "/posts/"+uuid.NewString()+"/album-backfills", input).Code)
	w = request(http.MethodPost, base+"/album-backfills", input)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var accepted gallery.AlbumBackfillStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &accepted))
	require.Equal(t, preview.Signature, accepted.Signature)
	require.False(t, accepted.PublicationCommitted)
	route := "/album-backfills/" + accepted.JobUUID
	require.Equal(t, http.StatusConflict, request(http.MethodPost, route+"/cancel", map[string]int64{"expected_revision": accepted.Revision + 1}).Code)
	require.Equal(t, http.StatusOK, request(http.MethodPost, route+"/cancel", map[string]int64{"expected_revision": accepted.Revision}).Code)
	w = request(http.MethodGet, route, nil)
	var cancelled gallery.AlbumBackfillStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cancelled))
	require.Equal(t, "cancelled", cancelled.State)
	retry := map[string]any{"request_uuid": uuid.NewString(), "expected_revision": cancelled.Revision}
	w = request(http.MethodPost, route+"/retry", retry)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var resumed gallery.AlbumBackfillStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resumed))
	started, release := make(chan struct{}), make(chan struct{})
	worker := gallery.NewAlbumWorker(s, func(ctx context.Context, pub gallery.AlbumPublication, guard gallery.AlbumEffectGuard) error {
		close(started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return guard(ctx)
		}
	})
	runtime := &archiveWorkerRuntime{worker: worker}
	t.Cleanup(runtime.stop)
	runtime.start()
	runtime.start()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("album worker did not reach committed effects")
	}
	w = request(http.MethodGet, "/album-backfills/"+resumed.JobUUID, nil)
	require.Contains(t, w.Body.String(), `"publication_committed":true`)
	require.Contains(t, w.Body.String(), `"hooks_finished":false`)
	close(release)
	require.Eventually(t, func() bool {
		w = request(http.MethodGet, "/album-backfills/"+resumed.JobUUID, nil)
		return bytes.Contains(w.Body.Bytes(), []byte(`"hooks_finished":true`))
	}, 10*time.Second, 20*time.Millisecond)
	runtime.stop()
	runtime.start()
	require.Equal(t, http.StatusOK, request(http.MethodPost, route+"/retry", retry).Code, "lost retry response returns its completed job")
	require.Contains(t, request(http.MethodGet, "/album-backfill-requests/"+input["request_uuid"], nil).Body.String(), `"state":"cancelled"`)
	require.Contains(t, request(http.MethodGet, "/album-backfills/"+resumed.JobUUID+"/attempts", nil).Body.String(), `"outcome":"succeeded"`)
	require.Contains(t, request(http.MethodGet, base+"/album-backfills?limit=1", nil).Body.String(), accepted.JobUUID)
	for _, query := range []string{"limit=101", "after=-1", "after=no", "limit=0"} {
		require.Equal(t, http.StatusBadRequest, request(http.MethodGet, base+"/album-backfills?"+query, nil).Code)
	}
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/album-backfills/"+uuid.NewString(), nil).Code)
	require.Equal(t, http.StatusNotFound, request(http.MethodPost, "/posts/"+uuid.NewString()+"/album-backfill/preview", previewInput).Code)
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, base+"/album-backfill/preview", map[string]string{"policy": "guess-by-title"}).Code)
	input["post_uuid"] = uuid.NewString()
	require.Equal(t, http.StatusBadRequest, request(http.MethodPost, base+"/album-backfills", input).Code)
	csrf := httptest.NewRequest(http.MethodPost, base+"/album-backfills", bytes.NewReader([]byte(`{}`)))
	csrf.Header.Set("Origin", "https://unrelated.test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, csrf)
	require.Equal(t, http.StatusForbidden, w.Code)

	// The separate producer router cannot access these application operations.
	var producer *models.IngestProducer
	var collection *models.SourceCollection
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Scoped scraper")
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
	for _, path := range []string{ingestPath + base + "/album-backfills", "/api/v3/archive" + base + "/album-backfills"} {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		producerHandler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	}
}
