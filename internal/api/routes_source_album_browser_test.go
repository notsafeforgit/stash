package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceAlbumReadChoiceConflictsReturn409(t *testing.T) {
	for _, err := range []error{models.ErrAttachmentSelectionConflict, models.ErrSourceGalleryConflict, models.ErrSourceAttachmentConflict} {
		t.Run(err.Error(), func(t *testing.T) {
			w := httptest.NewRecorder()
			nativeArchiveError(w, fmt.Errorf("read album: %w", err))
			require.Equal(t, http.StatusConflict, w.Code)
			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, "preview_changed", body["error"], "unsettled choices require review, not an outage retry")
		})
	}
}

func TestSourceAlbumBrowserHTTPOrderPaginationAndDisabledAssociation(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	var identity *models.ArchiveEntity
	gallery := models.NewGallery()
	gallery.Title = "Album"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "album"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", []byte(`{"category":"twitter","tweet_id":"album"}`))
		if err != nil {
			return err
		}
		capture, err := repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
			Origin: "gallery-dl", Platform: "twitter", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.RecordManifest(ctx, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, DeclaredAlbum: true,
			Entries: []models.SourceAttachmentEntry{{Position: 0, MediaKind: "image", Reference: models.SourcePostIdentifier{Namespace: "native:twitter", Value: "image"}},
				{Position: 2, MediaKind: "video", Reference: models.SourcePostIdentifier{Namespace: "native:twitter", Value: "video"}}}})
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
		if err := repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &gallery}); err != nil {
			return err
		}
		identity, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, gallery.ID)
		if err != nil {
			return err
		}
		post, err = repo.SourceEvidence.FindPost(ctx, post.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceGallery.DecideAssociation(ctx, models.SourceGalleryChoiceInput{PostUUID: post.UUID,
			ExpectedPostRevision: post.Revision, State: "linked", Origin: "review", GalleryUUID: identity.UUID, ExpectedGalleryRevision: identity.Revision})
		return err
	}))
	path := "/posts/" + post.UUID + "/album-media"
	var first, second models.SourceAlbumPage
	require.NoError(t, json.Unmarshal(get(path+"?limit=2", 200).Body.Bytes(), &first))
	require.Equal(t, post.UUID, first.RequestedUUID)
	require.Equal(t, post.UUID, first.PostUUID)
	require.Len(t, first.Slots, 2)
	require.Equal(t, 0, first.Slots[0].Position)
	require.Nil(t, first.Slots[1].Attachment)
	require.Equal(t, 1, *first.NextAfter)
	require.NoError(t, json.Unmarshal(get(path+"?after=1&limit=2", 200).Body.Bytes(), &second))
	require.Equal(t, first.Signature, second.Signature)
	require.Len(t, second.Slots, 1)
	require.Equal(t, "video", second.Slots[0].MediaKind)
	require.Equal(t, "unselected", second.Slots[0].SelectionState)
	require.Nil(t, second.NextAfter)
	var posts models.SourceGalleryPosts
	galleryPath := "/entities/" + identity.UUID + "/album-posts"
	require.NoError(t, json.Unmarshal(get(galleryPath, 200).Body.Bytes(), &posts))
	require.Equal(t, identity.UUID, posts.Gallery.UUID)
	require.Len(t, posts.Posts, 1)
	require.Equal(t, post.UUID, posts.Posts[0].UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceEvidence.FindPost(ctx, post.UUID)
		if err != nil {
			return err
		}
		_, err = repo.SourceGallery.DecideAssociation(ctx, models.SourceGalleryChoiceInput{PostUUID: post.UUID, ExpectedPostRevision: current.Revision,
			State: "disabled", Origin: "review"})
		return err
	}))
	var disabled models.SourceAlbumPage
	require.NoError(t, json.Unmarshal(get(path, 200).Body.Bytes(), &disabled))
	require.Equal(t, "disabled", disabled.Album.State)
	require.Len(t, disabled.Slots, 3)
	require.NotEqual(t, first.Signature, disabled.Signature)
	require.NoError(t, json.Unmarshal(get(galleryPath, 200).Body.Bytes(), &posts))
	require.Empty(t, posts.Posts, "a disabled association is not a current gallery link")
	for _, query := range []string{"limit=0", "limit=101", "after=-1", "after=1000000", "after=", "after=no", "after=1.5"} {
		get(path+"?"+query, 400)
	}
	get(galleryPath+"?after=bad", 400)
	get("/posts/bad/album-media", 400)
	get("/posts/"+uuid.NewString()+"/album-media", 404)
	get("/entities/"+uuid.NewString()+"/album-posts", 404)
}
