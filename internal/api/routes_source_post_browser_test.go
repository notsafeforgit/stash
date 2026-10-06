package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func sourcePostBrowserHTTPFixture(t *testing.T) (*sqlite.Database, models.Repository, func(string, int) *httptest.ResponseRecorder) {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "post-browser.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	handler := (&nativeArchiveRoutes{repo: db.Repository()}).router()
	get := func(path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, status, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		return w
	}
	return db, db.Repository(), get
}

func TestSourcePostBrowserHTTPExactLookupAndIdentifierPagination(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post-1"}, "")
		if err != nil {
			return err
		}
		for i := range 4 {
			current, err := repo.SourceEvidence.FindPost(ctx, post.UUID)
			if err != nil {
				return err
			}
			if err := repo.SourceEvidence.AddPostIdentifier(ctx, post.UUID,
				models.SourcePostIdentifier{Namespace: "native:reddit", Value: fmt.Sprint(i)}, current.Revision); err != nil {
				return err
			}
		}
		_, err = repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{
			SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "migration", Basis: "retained",
				ObservedAt: time.Now().UTC(), Details: []byte(`{}`)}, URL: "https://example.test/post?a=1&b=2"})
		return err
	}))
	base := "/posts/" + post.UUID
	var summary models.SourcePostSummary
	require.NoError(t, json.Unmarshal(get(base, 200).Body.Bytes(), &summary))
	require.Equal(t, post.UUID, summary.UUID)
	require.Len(t, summary.Identifiers, 3)
	require.True(t, summary.MoreIdentifiers)
	for _, query := range []string{"limit=1", "uuid=" + post.UUID, "namespace=native%3Atwitter&value=post-1", "url=" + url.QueryEscape(summary.URLs[0].URL)} {
		var rows []models.SourcePostSummary
		require.NoError(t, json.Unmarshal(get("/posts?"+query, 200).Body.Bytes(), &rows))
		require.Equal(t, []models.SourcePostSummary{summary}, rows)
	}
	require.JSONEq(t, `[]`, get("/posts?after="+post.UUID, 200).Body.String())
	var first, second []models.SourcePostIdentifierSummary
	require.NoError(t, json.Unmarshal(get(base+"/identifiers?limit=3", 200).Body.Bytes(), &first))
	require.Len(t, first, 3)
	cursor := url.Values{"limit": {"3"}, "after_namespace": {first[2].Namespace}, "after_value": {first[2].Value}}
	require.NoError(t, json.Unmarshal(get(base+"/identifiers?"+cursor.Encode(), 200).Body.Bytes(), &second))
	require.Len(t, second, 2)
	require.Equal(t, models.SourcePostIdentifierSummary{Namespace: "native:twitter", Value: "post-1"}, second[1])
	for _, query := range []string{"limit=0", "limit=101", "after=bad", "uuid=bad", "namespace=native%3Atwitter", "url=javascript%3Abad", "uuid=" + post.UUID + "&value=1"} {
		get("/posts?"+query, 400)
	}
	for _, query := range []string{"after_namespace=native%3Atwitter", "after_namespace=invalid&after_value=1"} {
		get(base+"/identifiers?"+query, 400)
	}
	get("/posts/invalid", 400)
	get("/posts/"+uuid.NewString(), 404)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		unchanged, err := repo.SourceEvidence.FindPost(ctx, post.UUID)
		require.NoError(t, err)
		require.Equal(t, summary.Revision, unchanged.Revision)
		return nil
	}))
}

func TestSourcePostBrowserHTTPPublishersRespectSelectionConsolidationAndUnlink(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	var capture *models.SourceCapture
	var selected, canonical *models.SourceAccount
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "publishers"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", []byte(`{"category":"twitter","tweet_id":"publishers","author":{"id":"123","name":"Publisher"}}`))
		if err != nil {
			return err
		}
		capture, err = repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "gallery-dl", Platform: "twitter", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		if err != nil {
			return err
		}
		preview, err := repo.CapturePublisher.Preview(ctx, capture.UUID, "")
		if err != nil {
			return err
		}
		decision, err := repo.CapturePublisher.Apply(ctx, models.CapturePublisherInput{UUID: uuid.NewString(), CaptureUUID: capture.UUID,
			ExpectedSignature: preview.Signature, Action: "automatic"})
		if err != nil {
			return err
		}
		selected, err = repo.SourceAccount.Find(ctx, *decision.AccountUUID)
		if err != nil {
			return err
		}
		unselected, err := repo.SourceAccount.Create(ctx, "native:twitter", "Claim only")
		if err != nil {
			return err
		}
		_, err = repo.SourcePostLinks.ClaimAccount(ctx, models.SourcePostAccountClaimInput{
			SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "migration", Basis: "retained",
				ObservedAt: time.Now().UTC(), Details: []byte(`{}`)}, AccountUUID: unselected.UUID})
		return err
	}))
	path := "/posts/" + post.UUID + "/publishers"
	read := func() []models.AccountReviewState {
		var rows []models.AccountReviewState
		require.NoError(t, json.Unmarshal(get(path+"?limit=1", 200).Body.Bytes(), &rows))
		return rows
	}
	rows := read()
	require.Len(t, rows, 1)
	require.Equal(t, selected.UUID, rows[0].UUID)
	require.Nil(t, rows[0].Ownership, "a selected publisher is not an inferred performer")
	require.JSONEq(t, `[]`, get(path+"?after="+selected.UUID, 200).Body.String())
	get(path+"?after=bad", 400)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		canonical, err = repo.SourceAccount.Create(ctx, "native:twitter", "Reviewed publisher")
		if err != nil {
			return err
		}
		preview, err := repo.SourceAccount.PreviewConsolidation(ctx, selected.UUID, canonical.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceAccount.Consolidate(ctx, models.AccountConsolidationInput{SourceUUID: selected.UUID, DestinationUUID: canonical.UUID,
			Signature: preview.Signature, OwnershipMode: "preserve", Origin: "review"})
		return err
	}))
	rows = read()
	require.Len(t, rows, 1)
	require.Equal(t, canonical.UUID, rows[0].UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		preview, err := repo.CapturePublisher.Preview(ctx, capture.UUID, "")
		if err != nil {
			return err
		}
		_, err = repo.CapturePublisher.Apply(ctx, models.CapturePublisherInput{UUID: uuid.NewString(), CaptureUUID: capture.UUID,
			ExpectedSignature: preview.Signature, Action: "unlink", Origin: "review"})
		return err
	}))
	require.Empty(t, read(), "retained claims cannot undo an explicit publisher unlink")
}

func TestSourcePostBrowserHTTPAlbumAndMediaInspectionPreservesChoices(t *testing.T) {
	db, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	var media, identity *models.ArchiveEntity
	gallery := models.NewGallery()
	gallery.Title = strings.Repeat("花", 600)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "album"}, "")
		if err != nil {
			return err
		}
		if _, _, err := db.ExecSQL(ctx, `INSERT INTO images(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, nil); err != nil {
			return err
		}
		media, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, 1)
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.RecordMediaEvidence(ctx, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID,
			MediaUUID: media.UUID, Basis: "legacy", Details: []byte(`{}`)})
		if err != nil {
			return err
		}
		if err := repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &gallery}); err != nil {
			return err
		}
		identity, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, gallery.ID)
		return err
	}))
	base := "/posts/" + post.UUID
	require.Equal(t, "null", strings.TrimSpace(get(base+"/album", 200).Body.String()), "inspection does not create an album")
	var items []models.SourcePostMediaItem
	require.NoError(t, json.Unmarshal(get(base+"/media", 200).Body.Bytes(), &items))
	require.Len(t, items, 1)
	require.Equal(t, "undecided", items[0].Association.State)
	require.Empty(t, items[0].Association.Decisions)
	require.True(t, items[0].HasRetainedEvidence)
	require.Equal(t, media.UUID, items[0].Media.UUID)
	require.JSONEq(t, `[]`, get(base+"/media?after="+media.UUID, 200).Body.String())
	get(base+"/media?after=bad", 400)
	choose := func(state string) {
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			current, err := repo.SourceEvidence.FindPost(ctx, post.UUID)
			if err != nil {
				return err
			}
			input := models.SourceGalleryChoiceInput{PostUUID: post.UUID, ExpectedPostRevision: current.Revision, State: state, Origin: "review"}
			if state == "linked" {
				input.GalleryUUID, input.ExpectedGalleryRevision = identity.UUID, identity.Revision
			}
			_, err = repo.SourceGallery.DecideAssociation(ctx, input)
			return err
		}))
	}
	readAlbum := func() models.SourcePostAlbum {
		var album models.SourcePostAlbum
		require.NoError(t, json.Unmarshal(get(base+"/album", 200).Body.Bytes(), &album))
		return album
	}
	choose("linked")
	album := readAlbum()
	require.Equal(t, "linked", album.State)
	require.Equal(t, identity.UUID, album.Gallery.UUID)
	require.Equal(t, gallery.ID, *album.Gallery.LocalID)
	require.True(t, album.Gallery.TitleTruncated)
	require.Equal(t, strings.Repeat("花", 512), album.Gallery.Title)
	require.Equal(t, album, readAlbum(), "inspection retains the exact association decision")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.Destroy(ctx, gallery.ID) }))
	album = readAlbum()
	require.Equal(t, models.ArchiveEntityDeleted, album.Gallery.State)
	require.Nil(t, album.Gallery.LocalID)
	choose("disabled")
	album = readAlbum()
	require.Equal(t, "disabled", album.State)
	require.Nil(t, album.Gallery)
	require.Equal(t, album, readAlbum())
	get("/posts/"+uuid.NewString()+"/album", 404)
}
