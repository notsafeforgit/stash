package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSourceAssociationReviewHTTPPreviewRecoveryHistoryAndRestore(t *testing.T) {
	db, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	var attachment *models.SourceAttachment
	var gallery, media *models.ArchiveEntity
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "association-review"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", []byte(`{"category":"twitter","tweet_id":"association-review"}`))
		if err != nil {
			return err
		}
		capture, err := repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "gallery-dl", Platform: "twitter", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.RecordManifest(ctx, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, DeclaredAlbum: true,
			Entries: []models.SourceAttachmentEntry{{Position: 0, MediaKind: "image", Reference: models.SourcePostIdentifier{Namespace: "native:twitter", Value: "converted"}}}})
		if err != nil {
			return err
		}
		post, err = repo.SourceEvidence.FindPost(ctx, post.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: post.Revision,
			CaptureUUID: capture.UUID, Mode: "pinned", Origin: "review"})
		if err != nil {
			return err
		}
		attachment, err = repo.SourceAttachment.Lookup(ctx, post.UUID, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "converted"})
		if err != nil {
			return err
		}
		g := models.NewGallery()
		g.Title = "Reviewed album"
		if err := repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &g}); err != nil {
			return err
		}
		gallery, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, g.ID)
		if err != nil {
			return err
		}
		scene := models.NewScene()
		scene.Title = "Converted animation"
		if err := repo.Scene.Create(ctx, &scene, nil); err != nil {
			return err
		}
		media, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveScene, scene.ID)
		if err != nil {
			return err
		}
		post, err = repo.SourceEvidence.FindPost(ctx, post.UUID)
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	send := func(path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		return w
	}
	galleryHistory := "/posts/" + post.UUID + "/gallery-association-history"
	mediaHistory := "/attachments/" + attachment.UUID + "/media-history"
	contextURL := "/attachments/" + attachment.UUID + "/review"
	for _, path := range []string{galleryHistory, mediaHistory} {
		require.JSONEq(t, "[]", get(path, 200).Body.String())
		get(path+"?limit=101", 400)
		get(path+"?after=-1", 400)
		get(path+"?after=invalid", 400)
	}
	get("/posts/"+uuid.NewString()+"/gallery-association-history", 404)
	get("/attachments/"+uuid.NewString()+"/media-history", 404)
	get("/attachments/"+uuid.NewString()+"/review", 404)
	get("/attachments/invalid/review", 400)
	for _, family := range []string{"gallery-association", "attachment-media"} {
		get("/"+family+"/requests/"+uuid.NewString(), 404)
		get("/"+family+"/requests/invalid", 400)
	}
	galleryInput := models.GalleryAssociationReviewInput{PostUUID: post.UUID, PostRevision: post.Revision, State: "linked",
		GalleryUUID: gallery.UUID, GalleryRevision: gallery.Revision, Reason: "Use this manual gallery"}
	var galleryPreview models.GalleryAssociationReviewPreview
	require.NoError(t, json.Unmarshal(send("/gallery-association/preview", galleryInput, 200).Body.Bytes(), &galleryPreview))
	require.Nil(t, galleryPreview.Current)
	require.Equal(t, gallery.UUID, galleryPreview.Proposed.UUID)
	require.JSONEq(t, "[]", get(galleryHistory, 200).Body.String())
	galleryRequest := models.GalleryAssociationReviewApplyInput{GalleryAssociationReviewInput: galleryInput, RequestUUID: uuid.NewString(), Digest: galleryPreview.Digest}
	var originalGallery models.GalleryAssociationReview
	for attempt := range 2 {
		var result struct {
			Review   models.GalleryAssociationReview `json:"review"`
			Replayed bool                            `json:"replayed"`
		}
		require.NoError(t, json.Unmarshal(send("/gallery-association/apply", galleryRequest, 200).Body.Bytes(), &result))
		require.Equal(t, attempt == 1, result.Replayed)
		if attempt == 0 {
			originalGallery = result.Review
		} else {
			require.Equal(t, originalGallery, result.Review)
		}
	}
	var galleryDecisions []models.SourcePostAlbum
	require.NoError(t, json.Unmarshal(get(galleryHistory+"?limit=1", 200).Body.Bytes(), &galleryDecisions))
	require.Len(t, galleryDecisions, 1)
	require.Equal(t, originalGallery.DecisionUUID, galleryDecisions[0].DecisionUUID)
	require.JSONEq(t, "[]", get(galleryHistory+"?after="+strconv.Itoa(galleryDecisions[0].Revision), 200).Body.String())
	staleGallery := galleryRequest
	staleGallery.RequestUUID = uuid.NewString()
	require.Contains(t, send("/gallery-association/apply", staleGallery, 409).Body.String(), "preview_changed")
	reusedGallery := galleryRequest
	reusedGallery.Reason = "Different intent"
	require.Contains(t, send("/gallery-association/apply", reusedGallery, 409).Body.String(), "request_conflict")
	invalidGallery := galleryInput
	invalidGallery.State = "disabled"
	send("/gallery-association/preview", invalidGallery, 400)
	send("/gallery-association/preview", map[string]any{"post_uuid": post.UUID, "post_revision": post.Revision, "state": "disabled", "origin": "ingest"}, 400)

	var current models.AttachmentMediaReviewContext
	w := get(contextURL, 200)
	require.NotContains(t, w.Body.String(), `"Namespace"`)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &current))
	require.Equal(t, post.Revision+1, current.PostRevision)
	require.Equal(t, []string{"image"}, current.SourceMediaKinds)
	require.Nil(t, current.Current)
	mediaInput := models.AttachmentMediaReviewInput{PostUUID: post.UUID, PostRevision: current.PostRevision,
		AttachmentUUID: attachment.UUID, AttachmentRevision: current.Attachment.Revision, State: "linked", MediaUUID: media.UUID, MediaRevision: media.Revision}
	var mediaPreview models.AttachmentMediaReviewPreview
	require.NoError(t, json.Unmarshal(send("/attachment-media/preview", mediaInput, 200).Body.Bytes(), &mediaPreview))
	require.Equal(t, models.ArchiveScene, mediaPreview.Proposed.Kind)
	require.JSONEq(t, "[]", get(mediaHistory, 200).Body.String())
	mediaRequest := models.AttachmentMediaReviewApplyInput{AttachmentMediaReviewInput: mediaInput, RequestUUID: uuid.NewString(), Digest: mediaPreview.Digest}
	var originalMedia models.AttachmentMediaReview
	for attempt := range 2 {
		var result struct {
			Review   models.AttachmentMediaReview `json:"review"`
			Replayed bool                         `json:"replayed"`
		}
		require.NoError(t, json.Unmarshal(send("/attachment-media/apply", mediaRequest, 200).Body.Bytes(), &result))
		require.Equal(t, attempt == 1, result.Replayed)
		if attempt == 0 {
			originalMedia = result.Review
		} else {
			require.Equal(t, originalMedia, result.Review)
		}
	}
	var mediaDecisions []models.AttachmentMediaReviewDecision
	require.NoError(t, json.Unmarshal(get(mediaHistory+"?limit=1", 200).Body.Bytes(), &mediaDecisions))
	require.Len(t, mediaDecisions, 1)
	require.Equal(t, originalMedia.DecisionUUID, mediaDecisions[0].UUID)
	require.JSONEq(t, "[]", get(mediaHistory+"?after="+strconv.Itoa(mediaDecisions[0].Revision), 200).Body.String())
	require.NoError(t, json.Unmarshal(get(contextURL, 200).Body.Bytes(), &current))
	require.Equal(t, originalMedia.DecisionUUID, current.Current.UUID)
	require.Equal(t, media.UUID, current.Media.UUID)
	staleMedia := mediaRequest
	staleMedia.RequestUUID = uuid.NewString()
	require.Contains(t, send("/attachment-media/apply", staleMedia, 409).Body.String(), "preview_changed")
	reusedMedia := mediaRequest
	reusedMedia.Reason = "Different intent"
	require.Contains(t, send("/attachment-media/apply", reusedMedia, 409).Body.String(), "request_conflict")
	invalidMedia := mediaInput
	invalidMedia.State = "unlinked"
	send("/attachment-media/preview", invalidMedia, 400)
	send("/attachment-media/preview", map[string]any{"post_uuid": post.UUID, "post_revision": current.PostRevision, "attachment_uuid": attachment.UUID,
		"attachment_revision": current.Attachment.Revision, "state": "undecided", "origin": "ingest"}, 400)
	for _, path := range []string{"/gallery-association/preview", "/gallery-association/apply", "/attachment-media/preview", "/attachment-media/apply"} {
		r := httptest.NewRequest(http.MethodPost, path, nil)
		r.Header.Set("Origin", "https://other.example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusForbidden, w.Code)
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	var recoveredGallery models.GalleryAssociationReview
	var recoveredMedia models.AttachmentMediaReview
	require.NoError(t, json.Unmarshal(get("/gallery-association/requests/"+galleryRequest.RequestUUID, 200).Body.Bytes(), &recoveredGallery))
	require.NoError(t, json.Unmarshal(get("/attachment-media/requests/"+mediaRequest.RequestUUID, 200).Body.Bytes(), &recoveredMedia))
	require.Equal(t, originalGallery, recoveredGallery)
	require.Equal(t, originalMedia, recoveredMedia)
	require.NoError(t, db.Close())
	restored := restoreSourceReview(t, db.DatabasePath(), "source_association_review_check.py")
	proof, err := sqlite.VerifyNativeSnapshot(t.Context(), restored)
	require.NoError(t, err)
	require.True(t, proof.DatabaseVerified)
	require.NoError(t, db.Open(restored))
	restoredRepo := db.Repository()
	require.NoError(t, restoredRepo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		g, err := restoredRepo.SourceGallery.AssociationReview(ctx, galleryRequest.RequestUUID)
		require.NoError(t, err)
		require.Equal(t, originalGallery, *g)
		m, err := restoredRepo.SourceAttachment.MediaReview(ctx, mediaRequest.RequestUUID)
		require.NoError(t, err)
		require.Equal(t, originalMedia, *m)
		return nil
	}))
}
