package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeSourceAssociationReviewSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeAttachmentDownloadSchema(t, raw)
	_, err := raw.Exec("DROP TABLE attachment_media_reviews; DROP TABLE gallery_association_reviews; DELETE FROM native_migration_history WHERE version=1000086")
	require.NoError(t, err)
}

func galleryReviewPreview(t *testing.T, repo models.Repository, input models.GalleryAssociationReviewInput) *models.GalleryAssociationReviewPreview {
	t.Helper()
	var result *models.GalleryAssociationReviewPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.SourceGallery.PreviewAssociationReview(ctx, input)
		return err
	}))
	return result
}

func galleryReviewApply(t *testing.T, repo models.Repository, input models.GalleryAssociationReviewApplyInput) (*models.GalleryAssociationReview, bool, error) {
	t.Helper()
	var result *models.GalleryAssociationReview
	var replayed bool
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = repo.SourceGallery.ApplyAssociationReview(ctx, input)
		return err
	})
	return result, replayed, err
}

func attachmentReviewPreview(t *testing.T, repo models.Repository, input models.AttachmentMediaReviewInput) *models.AttachmentMediaReviewPreview {
	t.Helper()
	var result *models.AttachmentMediaReviewPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.SourceAttachment.PreviewMediaReview(ctx, input)
		return err
	}))
	return result
}

func attachmentReviewApply(t *testing.T, repo models.Repository, input models.AttachmentMediaReviewApplyInput) (*models.AttachmentMediaReview, bool, error) {
	t.Helper()
	var result *models.AttachmentMediaReview
	var replayed bool
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, replayed, err = repo.SourceAttachment.ApplyMediaReview(ctx, input)
		return err
	})
	return result, replayed, err
}

func TestSourceGalleryReviewPreservesMembersAndOriginalReceipt(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
	gallery := createArchiveGallery(t, repo, "Manual collection")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.AddImages(ctx, gallery.ID, 41) }))
	identity := archiveFind(t, repo, models.ArchiveGallery, gallery.ID)
	input := models.GalleryAssociationReviewInput{PostUUID: post.UUID, PostRevision: post.Revision, State: "linked", GalleryUUID: identity.UUID, GalleryRevision: identity.Revision, Reason: "Reviewed association"}
	preview := galleryReviewPreview(t, repo, input)
	require.Nil(t, preview.Current)
	require.True(t, preview.Changed)
	require.Equal(t, identity.UUID, preview.Proposed.UUID)
	require.Equal(t, post.Revision, selectionPost(t, repo, post.UUID).Revision)
	request := models.GalleryAssociationReviewApplyInput{GalleryAssociationReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	result, replayed, err := galleryReviewApply(t, repo, request)
	require.NoError(t, err)
	require.False(t, replayed)
	require.Equal(t, request, result.Request)
	sourceGalleryMemberships(t, repo, gallery.ID, []int{41}, nil)
	fresh := input
	fresh.PostRevision = selectionPost(t, repo, post.UUID).Revision
	require.False(t, galleryReviewPreview(t, repo, fresh).Changed)
	// UUID adoption changes the retained FK but never the original request.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, identity.UUID, uuid.NewString(), identity.Revision)
		return err
	}))
	recovered, replayed, err := galleryReviewApply(t, repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, result, recovered)
	rejected := request
	rejected.Reason = "Different contents"
	_, _, err = galleryReviewApply(t, repo, rejected)
	require.ErrorIs(t, err, models.ErrSourceAssociationReviewReplay)
	disabled := models.GalleryAssociationReviewInput{PostUUID: post.UUID, PostRevision: selectionPost(t, repo, post.UUID).Revision, State: "disabled"}
	disabledPreview := galleryReviewPreview(t, repo, disabled)
	_, _, err = galleryReviewApply(t, repo, models.GalleryAssociationReviewApplyInput{GalleryAssociationReviewInput: disabled, RequestUUID: uuid.NewString(), Digest: disabledPreview.Digest})
	require.NoError(t, err)
	sourceGalleryMemberships(t, repo, gallery.ID, []int{41}, nil)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.Destroy(ctx, gallery.ID) }))
	attachmentSQL(t, db, "UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", post.UUID)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	recovered, replayed, err = galleryReviewApply(t, repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, result, recovered)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := repo.SourceGallery.AssociationHistory(ctx, post.UUID, 0, 25)
		require.NoError(t, err)
		require.Len(t, history, 2)
		require.Equal(t, "disabled", history[1].State)
		return nil
	}))
}

func TestAttachmentMediaReviewKeepsConvertedMediaAndOriginalReceipt(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
	entry := sourceAttachmentEntry(0, "converted")
	entry.MediaKind = "image"
	selected := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{entry}})
	attachment := selected.Entries[0].Attachment
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	input := models.AttachmentMediaReviewInput{PostUUID: post.UUID, PostRevision: selectionPost(t, repo, post.UUID).Revision,
		AttachmentUUID: attachment.UUID, AttachmentRevision: attachment.Revision, State: "linked", MediaUUID: media.UUID, MediaRevision: media.Revision}
	preview := attachmentReviewPreview(t, repo, input)
	require.Equal(t, []string{"image"}, preview.Current.SourceMediaKinds)
	require.Equal(t, models.ArchiveScene, preview.Proposed.Kind, "a converted source image can select a library video")
	require.Nil(t, preview.Current.Current)
	require.True(t, preview.Changed)
	request := models.AttachmentMediaReviewApplyInput{AttachmentMediaReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	result, replayed, err := attachmentReviewApply(t, repo, request)
	require.NoError(t, err)
	require.False(t, replayed)
	require.Equal(t, media.Revision, archiveFind(t, repo, models.ArchiveScene, 31).Revision)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		album, err := repo.SourceGallery.Association(ctx, post.UUID)
		require.NoError(t, err)
		require.Nil(t, album, "link review does not create a gallery")
		current, err := repo.SourceAttachment.MediaReviewContext(ctx, attachment.UUID)
		require.NoError(t, err)
		require.Equal(t, "linked", current.Current.State)
		require.Equal(t, media.UUID, current.Media.UUID)
		require.Equal(t, "undecided", current.PostLinkState)
		return nil
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, media.UUID, uuid.NewString(), media.Revision)
		return err
	}))
	recovered, replayed, err := attachmentReviewApply(t, repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, result, recovered)
	rejected := request
	rejected.Reason = "Different request"
	_, _, err = attachmentReviewApply(t, repo, rejected)
	require.ErrorIs(t, err, models.ErrSourceAssociationReviewReplay)
	unset := models.AttachmentMediaReviewInput{PostUUID: post.UUID, PostRevision: selectionPost(t, repo, post.UUID).Revision,
		AttachmentUUID: attachment.UUID, AttachmentRevision: attachment.Revision + 1, State: "unlinked"}
	unsetPreview := attachmentReviewPreview(t, repo, unset)
	_, _, err = attachmentReviewApply(t, repo, models.AttachmentMediaReviewApplyInput{AttachmentMediaReviewInput: unset, RequestUUID: uuid.NewString(), Digest: unsetPreview.Digest})
	require.NoError(t, err)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Scene.Destroy(ctx, 31) }))
	attachmentSQL(t, db, "UPDATE source_posts SET state='forgotten',revision=revision+1 WHERE uuid=?", post.UUID)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	recovered, replayed, err = attachmentReviewApply(t, repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, result, recovered)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := repo.SourceAttachment.MediaReviewHistory(ctx, attachment.UUID, 0, 25)
		require.NoError(t, err)
		require.Len(t, history, 2)
		require.Equal(t, "unlinked", history[1].State)
		return nil
	}))
}

func TestSourceAssociationReviewValidatesTargetsAndPreservesPostRejection(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
	selected := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "one")}})
	attachment := selected.Entries[0].Attachment
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	gallery := createArchiveGallery(t, repo, "Other kind")
	galleryIdentity := archiveFind(t, repo, models.ArchiveGallery, gallery.ID)
	base := models.AttachmentMediaReviewInput{PostUUID: post.UUID, PostRevision: selectionPost(t, repo, post.UUID).Revision,
		AttachmentUUID: attachment.UUID, AttachmentRevision: attachment.Revision, State: "linked", MediaUUID: media.UUID, MediaRevision: media.Revision}
	for _, scope := range []string{"foreign-post", "stale-media", "wrong-kind", "missing-attachment"} {
		t.Run(scope, func(t *testing.T) {
			input := base
			expected := models.ErrSourceAssociationReviewConflict
			switch scope {
			case "foreign-post":
				input.PostUUID, input.PostRevision = other.UUID, other.Revision
			case "stale-media":
				input.MediaRevision++
				expected = models.ErrSourceAttachmentConflict
			case "wrong-kind":
				input.MediaUUID, input.MediaRevision = galleryIdentity.UUID, galleryIdentity.Revision
				expected = models.ErrSourceAttachmentConflict
			case "missing-attachment":
				input.AttachmentUUID = uuid.NewString()
			}
			err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				_, err := repo.SourceAttachment.PreviewMediaReview(ctx, input)
				return err
			})
			require.ErrorIs(t, err, expected)
		})
	}
	for _, identity := range []*models.ArchiveEntity{media, galleryIdentity} {
		revision := identity.Revision
		if identity.Kind == models.ArchiveGallery {
			revision++
		}
		err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceGallery.PreviewAssociationReview(ctx, models.GalleryAssociationReviewInput{PostUUID: post.UUID, PostRevision: base.PostRevision,
				State: "linked", GalleryUUID: identity.UUID, GalleryRevision: revision})
			return err
		})
		require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	}
	require.Equal(t, base.PostRevision, selectionPost(t, repo, post.UUID).Revision)
	// A post-wide rejection is authoritative even when a slot has no decision.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post.UUID, MediaUUID: media.UUID,
			ExpectedPostRevision: base.PostRevision, ExpectedMediaRevision: media.Revision, ExpectedDecisions: []string{}, State: "unlinked", Origin: "review"})
		return err
	}))
	base.PostRevision = selectionPost(t, repo, post.UUID).Revision
	err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.PreviewMediaReview(ctx, base)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		choice, err := repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
		require.NoError(t, err)
		require.Nil(t, choice)
		association, err := repo.SourcePostMedia.Association(ctx, post.UUID, media.UUID)
		require.NoError(t, err)
		require.Equal(t, "unlinked", association.State)
		return nil
	}))
}

func TestSourceAssociationReviewAnonymisesAndRejectsCorruptReceiptsBeforeWrites(t *testing.T) {
	for _, family := range []string{"gallery_association", "attachment_media"} {
		t.Run(family, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
			selected := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "private")}})
			attachment := selected.Entries[0].Attachment
			galleryInput := models.GalleryAssociationReviewInput{PostUUID: post.UUID, PostRevision: selectionPost(t, repo, post.UUID).Revision, State: "disabled"}
			galleryPreview := galleryReviewPreview(t, repo, galleryInput)
			galleryRequest := models.GalleryAssociationReviewApplyInput{GalleryAssociationReviewInput: galleryInput, RequestUUID: uuid.NewString(), Digest: galleryPreview.Digest}
			_, _, err := galleryReviewApply(t, repo, galleryRequest)
			require.NoError(t, err)
			mediaInput := models.AttachmentMediaReviewInput{PostUUID: post.UUID, PostRevision: selectionPost(t, repo, post.UUID).Revision,
				AttachmentUUID: attachment.UUID, AttachmentRevision: attachment.Revision, State: "undecided"}
			mediaPreview := attachmentReviewPreview(t, repo, mediaInput)
			mediaRequest := models.AttachmentMediaReviewApplyInput{AttachmentMediaReviewInput: mediaInput, RequestUUID: uuid.NewString(), Digest: mediaPreview.Digest}
			_, _, err = attachmentReviewApply(t, repo, mediaRequest)
			require.NoError(t, err)
			_, _, err = repo.SourceGallery.ApplyAssociationReview(t.Context(), galleryRequest)
			require.Error(t, err, "replay still requires a managed transaction")
			_, _, err = repo.SourceAttachment.ApplyMediaReview(t.Context(), mediaRequest)
			require.Error(t, err, "replay still requires a managed transaction")
			path := filepath.Join(t.TempDir(), "anonymous.sqlite")
			anonymous, err := sqlite.NewAnonymiser(db, path)
			require.NoError(t, err)
			require.NoError(t, anonymous.Anonymise(t.Context()))
			raw := openRawDB(t, path)
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM gallery_association_reviews"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM attachment_media_reviews"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			require.NoError(t, raw.Close())
			require.NoError(t, db.Close())
			raw = openRawDB(t, db.DatabasePath())
			defer raw.Close()
			_, err = raw.Exec("UPDATE "+family+"_reviews SET signature=?", strings.Repeat("0", 64))
			require.ErrorContains(t, err, "immutable")
			var trigger string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", family+"_review_immutable").Scan(&trigger))
			_, err = raw.Exec("DROP TRIGGER " + family + "_review_immutable; UPDATE " + family + "_reviews SET signature='" + strings.Repeat("0", 64) + "'")
			require.NoError(t, err)
			_, err = raw.Exec(trigger)
			require.NoError(t, err)
			before, err := os.ReadFile(db.DatabasePath())
			require.NoError(t, err)
			require.ErrorContains(t, db.Open(db.DatabasePath()), "source association review")
			after, err := os.ReadFile(db.DatabasePath())
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestSourceAssociationReviewRejectsStaleScopesAndSwallowedFailures(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
	selected := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "one")}})
	attachment := selected.Entries[0].Attachment
	galleryInput := models.GalleryAssociationReviewInput{PostUUID: post.UUID, PostRevision: selectionPost(t, repo, post.UUID).Revision, State: "disabled"}
	galleryPreview := galleryReviewPreview(t, repo, galleryInput)
	galleryRequest := models.GalleryAssociationReviewApplyInput{GalleryAssociationReviewInput: galleryInput, RequestUUID: uuid.NewString(), Digest: galleryPreview.Digest}
	bad := galleryRequest
	bad.Digest = "0000000000000000000000000000000000000000000000000000000000000000"
	_, _, err := galleryReviewApply(t, repo, bad)
	require.ErrorIs(t, err, models.ErrSourceAssociationReviewConflict)
	mediaInput := models.AttachmentMediaReviewInput{PostUUID: post.UUID, PostRevision: galleryInput.PostRevision, AttachmentUUID: attachment.UUID, AttachmentRevision: attachment.Revision, State: "undecided"}
	mediaPreview := attachmentReviewPreview(t, repo, mediaInput)
	mediaRequest := models.AttachmentMediaReviewApplyInput{AttachmentMediaReviewInput: mediaInput, RequestUUID: uuid.NewString(), Digest: mediaPreview.Digest}
	for _, family := range []string{"gallery", "attachment"} {
		table := "gallery_association_reviews"
		if family == "attachment" {
			table = "attachment_media_reviews"
		}
		attachmentSQL(t, db, "CREATE TRIGGER fail_review BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'forced receipt failure'); END")
		err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			if family == "gallery" {
				_, _, err = repo.SourceGallery.ApplyAssociationReview(ctx, galleryRequest)
			} else {
				_, _, err = repo.SourceAttachment.ApplyMediaReview(ctx, mediaRequest)
			}
			require.ErrorContains(t, err, "forced receipt failure")
			return nil // A caught late error must not commit an unreceipted choice.
		})
		require.ErrorIs(t, err, models.ErrSourceAssociationReviewInvalid)
		require.Equal(t, galleryInput.PostRevision, selectionPost(t, repo, post.UUID).Revision)
		attachmentSQL(t, db, "DROP TRIGGER fail_review")
	}
	_, _, err = galleryReviewApply(t, repo, galleryRequest)
	require.NoError(t, err)
	_, _, err = attachmentReviewApply(t, repo, mediaRequest)
	require.ErrorIs(t, err, models.ErrSourceAssociationReviewConflict)
}

func TestSourceAssociationReviewMigrationPreservesExistingChoices(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "migration", true: "unknown-object"}[collision], func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			post, _ := singleSourceAlbum(t, repo)
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			galleryBefore := albumJobRows(t, raw, "post_gallery_decisions")
			mediaBefore := albumJobRows(t, raw, "attachment_media_decisions")
			removeSourceAssociationReviewSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000085,dirty=0")
			require.NoError(t, err)
			if collision {
				_, err = raw.Exec("CREATE TABLE attachment_media_reviews(original TEXT); INSERT INTO attachment_media_reviews VALUES('retain original')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			err = db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM attachment_media_reviews").Scan(&original))
				require.Equal(t, "retain original", original)
			} else {
				require.NoError(t, err)
				require.NoError(t, db.ReInitialise())
				require.NoError(t, db.Close())
				require.NoError(t, db.Open(db.DatabasePath()))
				require.Equal(t, "linked", sourceGalleryPreview(t, repo, post).Association.State)
			}
			require.Equal(t, galleryBefore, albumJobRows(t, raw, "post_gallery_decisions"))
			require.Equal(t, mediaBefore, albumJobRows(t, raw, "attachment_media_decisions"))
		})
	}
}
