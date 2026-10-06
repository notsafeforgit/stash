package sqlite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func comparePosts(t *testing.T, repo models.Repository, left, right string) *models.SourcePostComparison {
	t.Helper()
	var result *models.SourcePostComparison
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = repo.SourceEvidence.ComparePosts(ctx, left, right)
		return err
	}))
	return result
}

func comparisonIssueKinds(value *models.SourcePostComparison) []string {
	ret := []string{}
	for _, issue := range value.Conflicts {
		ret = append(ret, issue.Kind)
	}
	return ret
}

func comparisonURL(t *testing.T, repo models.Repository, post, sourceURL string) {
	t.Helper()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: models.SourcePostEvidence{
			UUID: uuid.NewString(), PostUUID: post, Origin: "migration", Basis: "retained", ObservedAt: time.Now().UTC()}, URL: sourceURL})
		return err
	}))
}

func TestSourcePostComparisonKeepsDistinctPostsWithSharedURLAndPayloads(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:instagram", Value: "story-one"}, "")
	b := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:instagram", Value: "story-two"}, "")
	for _, post := range []*models.SourcePost{a, b} {
		comparisonURL(t, repo, post.UUID, "https://www.instagram.com/stories/highlights/12345/")
		recordSourceTestCapture(t, repo, sourceTestCapture(t, post.UUID, 1, "private profile payload"))
	}
	beforeA, beforeB := selectionPost(t, repo, a.UUID), selectionPost(t, repo, b.UUID)
	read := comparePosts(t, repo, a.UUID, b.UUID)
	require.Equal(t, []string{"https://www.instagram.com/stories/highlights/12345/"}, read.SharedURLs)
	require.Equal(t, []string{"source_identifier"}, comparisonIssueKinds(read))
	require.Equal(t, []string{"story-one", "story-two"}, read.Conflicts[0].Values)
	require.Equal(t, "native:instagram", read.Conflicts[0].Namespace)
	require.NotEqual(t, read.Left.LatestCapture.UUID, read.Right.LatestCapture.UUID)
	require.Empty(t, read.Left.MediaChoices)
	require.Nil(t, read.Left.Selection)
	encoded, err := json.Marshal(read)
	require.NoError(t, err)
	for _, excluded := range []string{"private profile payload", "settings", "payload", "request_uuid", "ready", "digest"} {
		require.NotContains(t, string(encoded), excluded)
	}
	require.Equal(t, beforeA, selectionPost(t, repo, a.UUID))
	require.Equal(t, beforeB, selectionPost(t, repo, b.UUID))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, read, comparePosts(t, repo, a.UUID, b.UUID))
}

func TestSourcePostComparisonLegacyIdentityIsNotAnAutomaticMatch(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:example", Value: "old-key"}, "")
	b := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post-id"}, "")
	read := comparePosts(t, repo, a.UUID, b.UUID)
	require.Empty(t, read.Conflicts)
	require.Empty(t, read.SharedURLs)
	require.NotEqual(t, read.Left.UUID, read.Right.UUID)
	for _, identifiers := range [][]models.SourcePostIdentifierSummary{read.Left.Identifiers, read.Right.Identifiers} {
		require.Len(t, identifiers, 1)
	}
	comparisonURL(t, repo, a.UUID, "https://example.test/post")
	comparisonURL(t, repo, b.UUID, "https://example.test/post")
	read = comparePosts(t, repo, a.UUID, b.UUID)
	require.Len(t, read.SharedURLs, 1)
	require.Equal(t, a.UUID, read.Left.UUID)
	require.Equal(t, b.UUID, read.Right.UUID)
}

func TestSourcePostComparisonPreservesAttachmentOrderAndExplicitChoices(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:example", Value: "old-key"}, "")
	b := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post-id"}, "")
	for i, post := range []*models.SourcePost{a, b} {
		entries := []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "first"), sourceAttachmentEntry(1, "second")}
		if i == 1 {
			entries[0].Reference, entries[1].Reference = entries[1].Reference, entries[0].Reference
		}
		capture := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true, Entries: entries})
		applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision, Mode: "pinned", CaptureUUID: capture, Origin: "review"})
	}
	first := comparePosts(t, repo, a.UUID, b.UUID)
	require.Equal(t, []string{"source_list_position", "source_list_position"}, comparisonIssueKinds(first))
	require.Equal(t, 0, *first.Conflicts[0].Position)
	require.Equal(t, 1, *first.Conflicts[1].Position)
	image := archiveFind(t, repo, models.ArchiveImage, 41)
	attachment := first.Left.Attachments[0]
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
		ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: image.UUID, ExpectedMediaRevision: image.Revision, Origin: "review"}))
	other := first.Right.Attachments[0]
	require.NoError(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: other.UUID,
		ExpectedAttachmentRevision: other.Revision, State: "unlinked", Origin: "review"}))
	read := comparePosts(t, repo, a.UUID, b.UUID)
	require.Contains(t, comparisonIssueKinds(read), "attachment_choice")
	require.Equal(t, image.UUID, read.Left.Attachments[0].Media.UUID)
	require.Equal(t, "unlinked", read.Right.Attachments[0].Choice.State)
	require.Nil(t, read.Right.Attachments[0].Media)
	require.Equal(t, first.Left.Selection, read.Left.Selection, "media review does not rewrite source order")
	applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: a.UUID, ExpectedPostRevision: selectionPost(t, repo, a.UUID).Revision, Mode: "disabled", Origin: "review"})
	read = comparePosts(t, repo, a.UUID, b.UUID)
	require.Contains(t, comparisonIssueKinds(read), "source_list_mode")
	require.NotContains(t, comparisonIssueKinds(read), "source_list_position")
	require.Empty(t, read.Left.Selection.Entries)
	require.Len(t, read.Left.Attachments, 2, "disabled selection does not erase retained attachments or choices")
}

func TestSourcePostComparisonResolvesMediaMergesAndKeepsDeletedChoices(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:example", Value: "old-key"}, "")
	b := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post-id"}, "")
	first, second := archiveFind(t, repo, models.ArchiveScene, 31), archiveFind(t, repo, models.ArchiveScene, 32)
	_, err := applyPostMedia(repo, postMediaInput(t, repo, a.UUID, first.UUID, "linked"))
	require.NoError(t, err)
	_, err = applyPostMedia(repo, postMediaInput(t, repo, b.UUID, second.UUID, "unlinked"))
	require.NoError(t, err)
	require.Empty(t, comparePosts(t, repo, a.UUID, b.UUID).Conflicts)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{32}, 31); err != nil {
			return err
		}
		return repo.Scene.Destroy(ctx, 32)
	}))
	read := comparePosts(t, repo, a.UUID, b.UUID)
	require.Equal(t, []string{"media_choice"}, comparisonIssueKinds(read))
	require.Equal(t, first.UUID, read.Right.MediaChoices[0].Media.UUID)
	require.Equal(t, second.UUID, read.Right.MediaChoices[0].Decision.MediaUUID, "original decision retains its own identity")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Scene.Destroy(ctx, 31) }))
	attachmentSQL(t, db, "UPDATE source_posts SET state='forgotten' WHERE uuid=?", a.UUID)
	read = comparePosts(t, repo, a.UUID, b.UUID)
	require.Equal(t, models.ArchiveEntityDeleted, read.Left.MediaChoices[0].Media.State)
	require.Nil(t, read.Left.MediaChoices[0].Media.LocalID)
	require.Contains(t, comparisonIssueKinds(read), "post_not_active")
	require.Contains(t, comparisonIssueKinds(read), "media_choice")
}

func TestSourcePostComparisonEquivalentSourcesDoNotConflictOnAllocatedUUIDs(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:example", Value: "old-key"}, "")
	b := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post-id"}, "")
	for i, post := range []*models.SourcePost{a, b} {
		// The older list is partial. Missing observations do not contradict a
		// later complete list, and independent attachment UUIDs are not conflicts.
		input := models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "first")}}
		if i == 1 {
			input.Complete = true
			input.Entries = append(input.Entries, sourceAttachmentEntry(1, "second"))
		}
		selection := selectAlbum(t, repo, post.UUID, input)
		chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	}
	read := comparePosts(t, repo, a.UUID, b.UUID)
	require.Empty(t, read.Conflicts)
	require.NotEqual(t, read.Left.Selection.DecisionUUID, read.Right.Selection.DecisionUUID)
	require.NotEqual(t, read.Left.Attachments[0].UUID, read.Right.Attachments[0].UUID)
	require.Equal(t, read.Left.Attachments[0].Media.UUID, read.Right.Attachments[0].Media.UUID)
	require.NotZero(t, read.Left.Attachments[0].Choice.CreatedAt)
	require.False(t, read.Left.Selection.Complete)
	require.True(t, read.Right.Selection.Complete)
}

func TestSourcePostComparisonPreservesConflictingGalleriesAndDisabledChoice(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:example", Value: "old-key"}, "")
	b := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post-id"}, "")
	for _, post := range []*models.SourcePost{a, b} {
		selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true,
			Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "first")}})
		chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
		syncSourceGallery(t, repo, post.UUID)
	}
	read := comparePosts(t, repo, a.UUID, b.UUID)
	require.Equal(t, []string{"gallery_choice"}, comparisonIssueKinds(read))
	require.NotEqual(t, read.Left.Album.Gallery.UUID, read.Right.Album.Gallery.UUID)
	leftID, rightID := *read.Left.Album.Gallery.LocalID, *read.Right.Album.Gallery.LocalID
	sourceGalleryMemberships(t, repo, leftID, []int{41}, nil)
	sourceGalleryMemberships(t, repo, rightID, []int{41}, nil)
	post := selectionPost(t, repo, a.UUID)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceGallery.DecideAssociation(ctx, models.SourceGalleryChoiceInput{
			PostUUID: a.UUID, ExpectedPostRevision: post.Revision, State: "disabled", Origin: "review"})
		return err
	}))
	read = comparePosts(t, repo, a.UUID, b.UUID)
	require.Equal(t, "disabled", read.Left.Album.State)
	require.Nil(t, read.Left.Album.Gallery)
	require.Equal(t, []string{"gallery_choice"}, comparisonIssueKinds(read))
	sourceGalleryMemberships(t, repo, leftID, []int{41}, nil)
	sourceGalleryMemberships(t, repo, rightID, []int{41}, nil)
}

func TestSourcePostComparisonRejectsInvalidMissingAndOversizedScopes(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "a"}, "")
	b := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "b"}, "")
	for _, pair := range [][2]string{{"bad", b.UUID}, {a.UUID, a.UUID}, {a.UUID, uuid.Nil.String()}, {"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", b.UUID}} {
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceEvidence.ComparePosts(ctx, pair[0], pair[1])
			require.ErrorIs(t, err, models.ErrSourcePostComparisonInvalid)
			return nil
		}))
	}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.ComparePosts(ctx, a.UUID, uuid.NewString())
		require.ErrorIs(t, err, models.ErrSourcePostComparisonMissing)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	tx, err := raw.Begin()
	require.NoError(t, err)
	for i := range 512 {
		_, err := tx.Exec("INSERT INTO source_post_identifiers(namespace,value,post_uuid) VALUES(?,?,?)", "legacy:catalog:example", fmt.Sprint(i), a.UUID)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	require.NoError(t, raw.Close())
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		result, err := repo.SourceEvidence.ComparePosts(ctx, a.UUID, b.UUID)
		require.ErrorIs(t, err, models.ErrSourcePostComparisonLimit)
		require.Nil(t, result)
		return nil
	}))
}
