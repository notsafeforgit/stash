package sqlite_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceGalleryAdoptionRespectsClaimsThroughMergedAliases(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post, album := singleSourceAlbum(t, repo)
	old := archiveFind(t, repo, models.ArchiveGallery, *album.GalleryID)
	intermediate := createArchiveGallery(t, repo, "Intermediate gallery")
	middle := archiveFind(t, repo, models.ArchiveGallery, intermediate.ID)
	survivor := createArchiveGallery(t, repo, "Surviving gallery")
	target := archiveFind(t, repo, models.ArchiveGallery, survivor.ID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.ArchiveEntity.Redirect(ctx, old.UUID, middle.UUID, old.Revision); err != nil {
			return err
		}
		if err := repo.Gallery.Destroy(ctx, *album.GalleryID); err != nil {
			return err
		}
		if err := repo.ArchiveEntity.Redirect(ctx, middle.UUID, target.UUID, middle.Revision); err != nil {
			return err
		}
		return repo.Gallery.Destroy(ctx, intermediate.ID)
	}))
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
	target = archiveFind(t, repo, models.ArchiveGallery, survivor.ID)
	input := models.SourceGalleryChoiceInput{PostUUID: other.UUID, ExpectedPostRevision: other.Revision,
		State: "linked", GalleryUUID: target.UUID, ExpectedGalleryRevision: target.Revision, Origin: "review"}
	require.ErrorIs(t, decideSourceGallery(repo, input), models.ErrSourceGalleryConflict,
		"a retained post claim on a redirected gallery still owns the survivor")
	require.Equal(t, other.Revision, selectionPost(t, repo, other.UUID).Revision, "rejection leaves the post unchanged")
	require.Len(t, readGalleryPosts(t, repo, target.UUID, "", 25).Posts, 1)
	// The original owner may explicitly move its own historical association to
	// the survivor. This records a fresh choice without changing gallery members.
	input.PostUUID, input.ExpectedPostRevision = post, selectionPost(t, repo, post).Revision
	require.NoError(t, decideSourceGallery(repo, input))
	sourceGalleryMemberships(t, repo, survivor.ID, nil, nil)
	input.PostUUID, input.ExpectedPostRevision = other.UUID, other.Revision
	require.ErrorIs(t, decideSourceGallery(repo, input), models.ErrSourceGalleryConflict)
	// Explicitly disabling the original association releases its claim.
	require.NoError(t, decideSourceGallery(repo, models.SourceGalleryChoiceInput{
		PostUUID: post, ExpectedPostRevision: selectionPost(t, repo, post).Revision, State: "disabled", Origin: "review",
	}))
	require.NoError(t, decideSourceGallery(repo, input))
}

func TestSourceGalleryAdoptionBoundsRetainedAliasClaims(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
	gallery := createArchiveGallery(t, repo, "Large retained identity group")
	identity := archiveFind(t, repo, models.ArchiveGallery, gallery.ID)
	// A full permitted component is inspectable. One additional retained alias
	// must fail rather than letting a truncated scan declare the gallery free.
	attachmentSQL(t, db, `WITH RECURSIVE n(value) AS (
 VALUES(1) UNION ALL SELECT value+1 FROM n WHERE value<1023
 ) INSERT INTO archive_entities(uuid,kind,state,redirect_to,retired_at)
 SELECT printf('70000000-0000-4000-8000-%012d',value),'gallery','redirected',?,CURRENT_TIMESTAMP FROM n`, identity.UUID)
	input := models.SourceGalleryChoiceInput{PostUUID: post.UUID, ExpectedPostRevision: post.Revision, State: "linked",
		GalleryUUID: identity.UUID, ExpectedGalleryRevision: identity.Revision, Origin: "review"}
	require.NoError(t, decideSourceGallery(repo, input))
	attachmentSQL(t, db, `INSERT INTO archive_entities(uuid,kind,state,redirect_to,retired_at)
 VALUES('70000000-0000-4000-8000-000000001024','gallery','redirected',?,CURRENT_TIMESTAMP)`, identity.UUID)
	input.ExpectedPostRevision = selectionPost(t, repo, post.UUID).Revision
	require.ErrorIs(t, decideSourceGallery(repo, input), models.ErrSourceAlbumLimit)
	require.Equal(t, input.ExpectedPostRevision, selectionPost(t, repo, post.UUID).Revision)
}
