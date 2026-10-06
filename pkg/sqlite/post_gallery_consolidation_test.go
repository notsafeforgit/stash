package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func consolidationGalleryRequest(t *testing.T, repo models.Repository, post, gallery string) (models.SourceGalleryChoiceInput, []string) {
	t.Helper()
	input := models.SourceGalleryChoiceInput{PostUUID: post, State: "disabled", Origin: "review", Reason: "Reviewed consolidated album"}
	var expected []string
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.SourceEvidence.FindPost(ctx, post)
		require.NoError(t, err)
		input.ExpectedPostRevision = p.Revision
		if gallery != "" {
			g, err := repo.ArchiveEntity.Find(ctx, gallery)
			require.NoError(t, err)
			input.State, input.GalleryUUID, input.ExpectedGalleryRevision = "linked", g.UUID, g.Revision
		}
		heads, err := consolidatedPostGalleryHeads(ctx, post)
		require.NoError(t, err)
		for _, head := range heads {
			expected = append(expected, head.DecisionUUID)
		}
		return nil
	}))
	return input, expected
}

func consolidationGalleryChoice(t *testing.T, repo models.Repository, post string) *models.SourceGalleryDecision {
	t.Helper()
	var ret *models.SourceGalleryDecision
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.Association(ctx, post)
		return err
	}))
	return ret
}

func consolidationGallerySync(t *testing.T, repo models.Repository, post string) *models.SourceGallerySyncResult {
	t.Helper()
	var ret *models.SourceGallerySyncResult
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		preview, err := repo.SourceGallery.Preview(ctx, post)
		if err != nil {
			return err
		}
		ret, err = repo.SourceGallery.Sync(ctx, post, preview.Signature)
		return err
	}))
	return ret
}

func TestPostGalleryConsolidationPreservesHistoryManualMembersCoverAndMetadata(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	identityAlbum(t, repo, a)
	identityAlbum(t, repo, b)
	oldA, oldB := consolidationGalleryChoice(t, repo, a), consolidationGalleryChoice(t, repo, b)
	sourceScene := consolidationTestMedia(t, repo)
	manualScene := consolidationTestMedia(t, repo)
	var galleryID int
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "INSERT INTO images(id,created_at,updated_at) VALUES(41,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
		require.NoError(t, err)
		cover, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveImage, 41)
		require.NoError(t, err)
		for _, item := range []struct {
			key   string
			media *models.ArchiveEntity
		}{{"first", cover}, {"second", sourceScene}} {
			attachment, err := repo.SourceAttachment.Lookup(ctx, a, models.SourcePostIdentifier{Namespace: "native:reddit", Value: item.key})
			require.NoError(t, err)
			_, err = repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
				ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: item.media.UUID,
				ExpectedMediaRevision: item.media.Revision, Origin: "review"})
			require.NoError(t, err)
		}
		preview, err := repo.SourceGallery.Preview(ctx, a)
		require.NoError(t, err)
		_, err = repo.SourceGallery.Sync(ctx, a, preview.Signature)
		require.NoError(t, err)
		galleryID = *preview.Gallery.LocalID
		require.NoError(t, repo.Gallery.AddSceneIDs(ctx, galleryID, []int{*manualScene.LocalID}))
		require.NoError(t, repo.Gallery.SetCover(ctx, galleryID, 41))
		_, err = repo.Gallery.UpdatePartial(ctx, galleryID, models.GalleryPartial{
			Title: models.NewOptionalString("My album"), Details: models.NewOptionalString("My description")})
		return err
	}))
	unchangedTables := []string{"galleries", "galleries_images", "scenes_galleries", "gallery_membership_events", "metadata_field_decisions"}
	before := identityRows(t, repo, unchangedTables...)
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	// This later source list no longer includes either automatic member.
	// The chosen cover and manual extra must nevertheless stay in the gallery.
	later := postSelectionCapture(t, repo, b, 2)
	postSelectionApply(t, repo, b, later.UUID, "pinned", "review")
	input, expected := consolidationGalleryRequest(t, repo, b, *oldA.GalleryUUID)
	require.ElementsMatch(t, []string{oldA.UUID, oldB.UUID}, expected)
	var selected *models.SourceGalleryDecision
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		selected, err = publishConsolidatedPostGallery(ctx, input, merge.UUID, expected)
		return err
	}))
	require.Equal(t, before, identityRows(t, repo, unchangedTables...), "adoption changes associations, not gallery contents")
	require.Nil(t, consolidationGalleryChoice(t, repo, a))
	require.Equal(t, selected, consolidationGalleryChoice(t, repo, b))
	require.Equal(t, oldA.GalleryUUID, selected.GalleryUUID)
	result := consolidationGallerySync(t, repo, b)
	require.Equal(t, []string{sourceScene.UUID}, result.Removed, "old source-owned membership follows the consolidated identity")
	require.Empty(t, result.Added)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		images, err := repo.Gallery.GetImageIDs(ctx, galleryID)
		require.NoError(t, err)
		require.Equal(t, []int{41}, images)
		scenes, err := repo.Gallery.GetSceneIDs(ctx, galleryID)
		require.NoError(t, err)
		require.Equal(t, []int{*manualScene.LocalID}, scenes)
		gallery, err := repo.Gallery.Find(ctx, galleryID)
		require.NoError(t, err)
		require.Equal(t, "My album", gallery.Title)
		require.Equal(t, "My description", gallery.Details)
		for _, original := range []*models.SourceGalleryDecision{oldA, oldB} {
			history, err := repo.SourceGallery.AssociationHistory(ctx, original.PostUUID, 0, 100)
			require.NoError(t, err)
			require.Equal(t, *original, history[0])
			identity, err := repo.ArchiveEntity.Find(ctx, *original.GalleryUUID)
			require.NoError(t, err)
			require.Equal(t, models.ArchiveEntityActive, identity.State, "unselected galleries are not deleted")
		}
		return nil
	}))
	// Clearing the explicit cover protection makes that original source member
	// removable, while a manual member remains protected after the merge.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.ResetCover(ctx, galleryID) }))
	require.Len(t, consolidationGallerySync(t, repo, b).Removed, 1)
}

func TestPostGalleryConsolidationRejectsStaleReviewAndOutsideGalleryClaimAtomically(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one-post")
	other := identityPost(t, repo, "native:reddit", "other-post")
	for _, post := range []string{a, b, other} {
		identityAlbum(t, repo, post)
	}
	oldA := consolidationGalleryChoice(t, repo, a)
	outside := consolidationGalleryChoice(t, repo, other)
	merge := publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	input, expected := consolidationGalleryRequest(t, repo, b, *oldA.GalleryUUID)
	before := identityRows(t, repo, "source_posts", "post_gallery_links", "post_gallery_decisions", "galleries")
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedPostGallery(ctx, input, merge.UUID, expected[:1])
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	require.Equal(t, before, identityRows(t, repo, "source_posts", "post_gallery_links", "post_gallery_decisions", "galleries"))
	input, expected = consolidationGalleryRequest(t, repo, b, *outside.GalleryUUID)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedPostGallery(ctx, input, merge.UUID, expected)
		require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
		return nil // Catching the late conflict must not commit retired heads.
	})
	require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	require.Equal(t, before, identityRows(t, repo, "source_posts", "post_gallery_links", "post_gallery_decisions", "galleries"))
	// Choosing no automatic gallery retires both heads but preserves the albums.
	input, expected = consolidationGalleryRequest(t, repo, b, "")
	galleries := identityRows(t, repo, "galleries")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishConsolidatedPostGallery(ctx, input, merge.UUID, expected)
		return err
	}))
	require.Equal(t, galleries, identityRows(t, repo, "galleries"))
	require.Nil(t, consolidationGalleryChoice(t, repo, a))
	require.Equal(t, "disabled", consolidationGallerySync(t, repo, b).Action)
}

func TestPostGalleryConsolidationChoiceLookupUsesCanonicalGroupAndPostIndexes(t *testing.T) {
	_, repo := postIdentityFixture(t)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var rows []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+consolidatedPostGalleriesQuery, "root", maxPostIdentityMembers+1))
		plan := fmt.Sprint(rows)
		require.Contains(t, plan, "SEARCH i USING COVERING INDEX source_post_identities_canonical (canonical_uuid=?)")
		require.Contains(t, plan, "SEARCH l USING PRIMARY KEY (post_uuid=?)")
		require.NotContains(t, plan, "SCAN ")
		require.NotContains(t, plan, "TEMP B-TREE")
		return nil
	}))
}
