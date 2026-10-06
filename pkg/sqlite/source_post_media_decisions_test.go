package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func postMediaAssociation(t *testing.T, repo models.Repository, post, media string) *models.SourcePostMediaAssociation {
	t.Helper()
	var ret *models.SourcePostMediaAssociation
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourcePostMedia.Association(ctx, post, media)
		return err
	}))
	return ret
}

func postMediaInput(t *testing.T, repo models.Repository, post, media, state string) models.SourcePostMediaInput {
	t.Helper()
	a := postMediaAssociation(t, repo, post, media)
	input := models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post, MediaUUID: a.MediaUUID, ExpectedPostRevision: a.PostRevision, ExpectedMediaRevision: a.MediaRevision, State: state, Origin: "review", Reason: "Reviewed source post"}
	for _, d := range a.Decisions {
		input.ExpectedDecisions = append(input.ExpectedDecisions, d.UUID)
	}
	return input
}

func applyPostMedia(repo models.Repository, input models.SourcePostMediaInput) (*models.SourcePostMediaDecision, error) {
	var ret *models.SourcePostMediaDecision
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourcePostMedia.Decide(ctx, input)
		return err
	})
	return ret, err
}

func TestPostMediaHistoricalCapturePoliciesWithoutAttachments(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "nfo-post"}, "")
	capture := recordSourceTestCapture(t, f.repo, sourceTestCapture(t, post.UUID, 1, "retained profile"))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	scope := models.MetadataPolicySampleScope{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, EntityUUID: f.entity.UUID}
	samples := func(after *models.MetadataPolicySourceCursor) []models.MetadataPolicySampleSource {
		var rows []models.MetadataPolicySampleSource
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			rows, err = f.repo.MetadataPolicy.SampleSources(ctx, scope, after, 1)
			return err
		}))
		return rows
	}
	recordMediaEvidence(t, f.repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, CaptureUUID: capture.UUID, MediaUUID: f.entity.UUID, Basis: "legacy", Details: []byte(`{}`)})
	require.Empty(t, samples(nil), "retained evidence does not silently select the association")
	request := postMediaInput(t, f.repo, post.UUID, f.entity.UUID, "linked")
	decision, err := applyPostMedia(f.repo, request)
	require.NoError(t, err)
	rows := samples(nil)
	require.Len(t, rows, 1)
	require.Equal(t, capture.UUID, rows[0].CaptureUUID)
	require.Equal(t, decision.UUID, rows[0].PostMediaDecisionUUID)
	require.Empty(t, rows[0].AttachmentUUID)
	require.Empty(t, samples(&rows[0].MetadataPolicySourceCursor))
	rule := models.MetadataPolicyRule{OnExisting: true, OnCreate: true, Mappings: map[string]models.MetadataMapping{"title": {JQ: `.source.metadata.title`}}}
	policy := f.put(t, 0, rule)
	input := f.input(policy)
	input.Source = &metadata.Source{CaptureUUID: capture.UUID, PostMediaDecisionUUID: decision.UUID}
	preview := policyPreview(t, f.repo, input)
	require.JSONEq(t, `"Shared title"`, string(policyChange(t, preview, "title").Value))
	require.Equal(t, decision.UUID, policyChange(t, preview, "title").PostMediaDecisionUUID)
	_, err = applyPolicy(f.repo, input, preview.Digest)
	require.NoError(t, err)
	state := metadataState(t, f.repo, f.entity.UUID, "title")
	require.Equal(t, decision.UUID, state.Decision.PostMediaDecisionUUID)
	require.Equal(t, capture.UUID, *state.Decision.CaptureUUID)
	// Reopen verifies both selected-field provenance and exact request recovery.
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	replayed, err := applyPostMedia(f.repo, request)
	require.NoError(t, err)
	require.Equal(t, decision, replayed)
	require.Equal(t, decision.UUID, metadataState(t, f.repo, f.entity.UUID, "title").Decision.PostMediaDecisionUUID)
	changed := request
	changed.State = "unlinked"
	_, err = applyPostMedia(f.repo, changed)
	require.ErrorIs(t, err, models.ErrSourcePostMediaReplay)
	_, err = applyPostMedia(f.repo, postMediaInput(t, f.repo, post.UUID, f.entity.UUID, "unlinked"))
	require.NoError(t, err)
	require.Empty(t, samples(nil))
	_, err = applyPolicy(f.repo, input, "")
	require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
	require.Equal(t, decision.UUID, metadataState(t, f.repo, f.entity.UUID, "title").Decision.PostMediaDecisionUUID, "unlink retains previously selected metadata provenance")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	for _, table := range []string{"source_attachments", "source_attachment_manifests", "galleries"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anonymous := openRawDB(t, output)
	defer anonymous.Close()
	for _, table := range []string{"post_media_decisions", "post_media_links", "post_media_supersessions", "metadata_decision_post_media"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table), table)
	}
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestPostMediaMergeConflictsRequireReviewAndRetainHistory(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "merge"}, "")
	one, two := archiveFind(t, repo, models.ArchiveScene, 31), archiveFind(t, repo, models.ArchiveScene, 32)
	first, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, one.UUID, "linked"))
	require.NoError(t, err)
	_, err = applyPostMedia(repo, postMediaInput(t, repo, post.UUID, two.UUID, "unlinked"))
	require.NoError(t, err)
	stale := postMediaInput(t, repo, post.UUID, two.UUID, "linked")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
			return err
		}
		return repo.Scene.Destroy(ctx, 31)
	}))
	a := postMediaAssociation(t, repo, post.UUID, two.UUID)
	require.Equal(t, "conflict", a.State)
	require.Len(t, a.Decisions, 2)
	// Even if a merge caller did not edit the surviving row's metadata,
	// newly combined choices invalidate the saved association review.
	stale.ExpectedMediaRevision = a.MediaRevision
	_, err = applyPostMedia(repo, stale)
	require.ErrorIs(t, err, models.ErrSourcePostMediaConflict)
	resolved, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, two.UUID, "linked"))
	require.NoError(t, err)
	a = postMediaAssociation(t, repo, post.UUID, one.UUID)
	require.Equal(t, "linked", a.State)
	require.Len(t, a.Decisions, 1)
	require.Equal(t, resolved.UUID, a.Decisions[0].UUID)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := repo.SourcePostMedia.History(ctx, post.UUID, two.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 3)
		require.Equal(t, first.UUID, history[0].UUID)
		page, err := repo.SourcePostMedia.History(ctx, post.UUID, two.UUID, history[1].PostRevision, 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourcePostMediaDecision{*resolved}, page)
		return nil
	}))
	adopted := uuid.NewString()
	two = archiveFind(t, repo, models.ArchiveScene, 32)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, two.UUID, adopted, two.Revision)
		return err
	}))
	require.Equal(t, adopted, postMediaAssociation(t, repo, post.UUID, two.UUID).MediaUUID)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, "linked", postMediaAssociation(t, db.Repository(), post.UUID, one.UUID).State)
}

func TestPostMediaUnlinkSuppressesSourceAlbumButPreservesManualMembership(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "album-unlink"}, "")
	selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "clip")}})
	attachment := selection.Entries[0].Attachment
	media := chooseAlbumMedia(t, repo, attachment.UUID, models.ArchiveScene, 31)
	album := syncSourceGallery(t, repo, post.UUID)
	sourceGalleryMemberships(t, repo, *album.GalleryID, nil, []int{31})
	_, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, media.UUID, "unlinked"))
	require.NoError(t, err)
	sourceGalleryMemberships(t, repo, *album.GalleryID, nil, nil)
	require.Equal(t, "post_unlinked", sourceGalleryPreview(t, repo, post.UUID).Entries[0].Status)
	current := findAttachment(t, repo, attachment.UUID)
	media = archiveFind(t, repo, models.ArchiveScene, 31)
	require.ErrorIs(t, applyMediaChoice(repo, models.AttachmentMediaDecisionInput{AttachmentUUID: current.UUID, ExpectedAttachmentRevision: current.Revision, State: "linked", MediaUUID: media.UUID, ExpectedMediaRevision: media.Revision, Origin: "review"}), models.ErrSourcePostMediaConflict)
	_, err = applyPostMedia(repo, postMediaInput(t, repo, post.UUID, media.UUID, "undecided"))
	require.NoError(t, err)
	sourceGalleryMemberships(t, repo, *album.GalleryID, nil, []int{31})
	// An explicitly added library membership survives a later source rejection.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.AddSceneIDs(ctx, *album.GalleryID, []int{31}) }))
	_, err = applyPostMedia(repo, postMediaInput(t, repo, post.UUID, media.UUID, "unlinked"))
	require.NoError(t, err)
	sourceGalleryMemberships(t, repo, *album.GalleryID, nil, []int{31})
}

func TestPostMediaUnlinkPreventsHistoricalAlbumSelection(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	media := archiveFind(t, f.repo, models.ArchiveScene, 31)
	before := albumBackfillPreview(t, f.repo, post, models.SourceAlbumRedditFilenameV1)
	require.Equal(t, "matched", before.Matches[0].Status)
	_, err := applyPostMedia(f.repo, postMediaInput(t, f.repo, post, media.UUID, "unlinked"))
	require.NoError(t, err)
	after := albumBackfillPreview(t, f.repo, post, before.Policy)
	require.Equal(t, "review", after.Matches[0].Status)
	require.Equal(t, "post-media-unlinked", after.Matches[0].Reason)
	require.NotEqual(t, before.Signature, after.Signature)
	result := applyAlbumBackfill(t, f.repo, after)
	require.Zero(t, result.Selected)
	require.Equal(t, 1, result.Review)
}
