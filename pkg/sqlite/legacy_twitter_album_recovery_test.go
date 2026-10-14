package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

const recoveryTweet = "1575550205214134278"

func legacyRecoveryFixture(t *testing.T) (sourceFileFixture, sqlite.LegacyTwitterRecovery) {
	t.Helper()
	f := newSourceFileFixture(t)
	attachmentSQL(t, f.db, `UPDATE folders SET path='/library/example, twitter' WHERE id=1;
 UPDATE files SET basename='1575550205214134278_3.mkv' WHERE id=21;
 INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(31,21,1);
 INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at)
 VALUES(22,1,'1575550205214134278_1.jpg',1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO images_files(image_id,file_id,"primary") VALUES(41,22,1);`)
	return f, sqlite.LegacyTwitterRecovery{TweetID: recoveryTweet, Evidence: []sqlite.LegacyTwitterFile{
		{FileUUID: archiveFind(t, f.repo, models.ArchiveFile, 22).UUID, MediaUUID: archiveFind(t, f.repo, models.ArchiveImage, 41).UUID, Generation: 1},
		{FileUUID: archiveFind(t, f.repo, models.ArchiveFile, 21).UUID, MediaUUID: archiveFind(t, f.repo, models.ArchiveScene, 31).UUID, Generation: 3},
	}}
}

func TestLegacyTwitterRecoveryMixedOrphansAndReplay(t *testing.T) {
	f, input := legacyRecoveryFixture(t)
	preview, err := f.db.PreviewLegacyTwitterRecovery(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, "ready", preview.State)
	require.Empty(t, preview.Fragments)
	post, err := f.db.ApplyLegacyTwitterRecovery(t.Context(), input, preview.Signature)
	require.NoError(t, err)
	selected := filenameSelectionGet(t, f.repo, post)
	require.Len(t, selected.Entries, 2)
	require.Equal(t, 0, selected.Entries[0].Position)
	require.Equal(t, 2, selected.Entries[1].Position)
	require.Equal(t, "video", selected.Entries[1].MediaKind)
	require.Nil(t, selected.ExpectedCount)
	require.False(t, selected.Complete)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		capture, err := f.repo.SourceEvidence.FindCapture(ctx, *selected.Decision.CaptureUUID)
		require.NoError(t, err)
		require.Equal(t, "filename-recovery", capture.Origin)
		require.True(t, capture.CapturedAt.IsZero())
		require.Nil(t, capture.Metadata.OriginalText)
		return nil
	}))
	albumPreview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumIdentifiersV1)
	require.Equal(t, "Twitter post "+recoveryTweet, albumPreview.Gallery.Title)
	album := applyAlbumBackfill(t, f.repo, albumPreview)
	sourceGalleryMemberships(t, f.repo, *album.Gallery.GalleryID, []int{41}, []int{31})
	replayed, err := f.db.ApplyLegacyTwitterRecovery(t.Context(), input, preview.Signature)
	require.NoError(t, err)
	require.Equal(t, post, replayed)
	backup := filepath.Join(t.TempDir(), "recovered.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.AuditForTesting(backup))
}

func TestLegacyTwitterRecoveryCollatesOriginalFragmentsAfterDeduplication(t *testing.T) {
	f := newSourceFileFixture(t)
	input := sqlite.LegacyTwitterRecovery{TweetID: recoveryTweet}
	recordContentClaim(t, f.repo, f.claim)
	for index, kind := range []models.ArchiveEntityKind{models.ArchiveImage, models.ArchiveScene} {
		post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: string(kind)}, "")
		recordSourceTestCapture(t, f.repo, sourceTestCapture(t, post.UUID, 1, "profile"))
		mediaID, fileID, relative := 41, 22, "example, twitter/"+recoveryTweet+"_1.jpg"
		if index == 1 {
			mediaID, fileID, relative = 31, 21, "example, twitter/"+recoveryTweet+"_3.gif"
		}
		evidence := albumLegacyEvidence(t, f, post.UUID, relative, nil, kind, mediaID, fileID)
		input.Evidence = append(input.Evidence, sqlite.LegacyTwitterFile{FileUUID: *evidence.FileUUID, MediaUUID: evidence.MediaUUID, Generation: 1})
		linkAlbumPostMedia(t, f.repo, post.UUID, kind, mediaID)
		var details map[string]string
		require.NoError(t, json.Unmarshal(evidence.Details, &details))
		require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			proofs, err := f.repo.SourceFile.PostEvidence(ctx, post.UUID, "", 10)
			require.NoError(t, err)
			require.Len(t, proofs, 1)
			input.Evidence[index].ObservationUUID = proofs[0].ObservationUUID
			input.Evidence[index].MatchUUID = details["source_file_match_uuid"]
			return nil
		}))
	}
	preview, err := f.db.PreviewLegacyTwitterRecovery(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, preview.Fragments, 2)
	post, err := f.db.ApplyLegacyTwitterRecovery(t.Context(), input, preview.Signature)
	require.NoError(t, err)
	require.Equal(t, preview.Fragments[0].UUID, post)
	album := applyAlbumBackfill(t, f.repo, albumBackfillPreview(t, f.repo, post, models.SourceAlbumIdentifiersV1))
	sourceGalleryMemberships(t, f.repo, *album.Gallery.GalleryID, []int{41}, []int{31})
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		identity, err := f.repo.SourceEvidence.PostIdentity(ctx, preview.Fragments[1].UUID)
		require.NoError(t, err)
		require.Equal(t, post, identity.CanonicalUUID)
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestLegacyTwitterRecoveryRejectsChangedFilesAndAmbiguity(t *testing.T) {
	for _, scenario := range []string{"generation", "folder", "wrong post", "single first slot"} {
		t.Run(scenario, func(t *testing.T) {
			f, input := legacyRecoveryFixture(t)
			preview, err := f.db.PreviewLegacyTwitterRecovery(t.Context(), input)
			require.NoError(t, err)
			switch scenario {
			case "generation":
				attachmentSQL(t, f.db, "UPDATE files SET generation=generation+1 WHERE id=21")
			case "folder":
				attachmentSQL(t, f.db, "UPDATE folders SET path='/unrelated/files' WHERE id=1")
			case "wrong post":
				input.TweetID = "1575550205214134279"
			case "single first slot":
				input.Evidence = input.Evidence[:1]
			}
			_, err = f.db.ApplyLegacyTwitterRecovery(t.Context(), input, preview.Signature)
			require.Error(t, err)
		})
	}
}

func TestLegacyTwitterRecoveryRepeatedOrdinalKeepsMixedMembersWithoutInventingOrder(t *testing.T) {
	f, input := legacyRecoveryFixture(t)
	attachmentSQL(t, f.db, "UPDATE files SET basename='1575550205214134278_1.mkv' WHERE id=21")
	input.Evidence[1].Generation = 4
	preview, err := f.db.PreviewLegacyTwitterRecovery(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, []int{0}, preview.ConflictingPositions)
	post, err := f.db.ApplyLegacyTwitterRecovery(t.Context(), input, preview.Signature)
	require.NoError(t, err)
	selected := filenameSelectionGet(t, f.repo, post)
	require.Empty(t, selected.Entries)
	require.True(t, selected.DeclaredAlbum)
	require.Nil(t, selected.ExpectedCount)
	require.False(t, selected.Complete)
	album := applyAlbumBackfill(t, f.repo, albumBackfillPreview(t, f.repo, post, models.SourceAlbumIdentifiersV1))
	sourceGalleryMemberships(t, f.repo, *album.Gallery.GalleryID, []int{41}, []int{31})
	for _, file := range input.Evidence {
		require.Equal(t, "linked", postMediaAssociation(t, f.repo, post, file.MediaUUID).State)
	}
	backup := filepath.Join(t.TempDir(), "ambiguous-order.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.AuditForTesting(backup))
}

func TestLegacyTwitterRecoveryPreservesExplicitChoicesAndDistinctPosts(t *testing.T) {
	for _, choice := range []string{"disabled", "unlinked", "different tweet"} {
		t.Run(choice, func(t *testing.T) {
			f, input := legacyRecoveryFixture(t)
			recordContentClaim(t, f.repo, f.claim)
			post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "fragment"}, "")
			recordSourceTestCapture(t, f.repo, sourceTestCapture(t, post.UUID, 1, "profile"))
			observation := f.observation
			observation.UUID, observation.RelativePath = uuid.NewString(), "example, twitter/"+recoveryTweet+"_1.jpg"
			if choice == "different tweet" {
				observation.RelativePath = "example, twitter/1575550205214134279_1.jpg"
			}
			recordFileObservation(t, f.repo, observation)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.SourceFile.RecordPostEvidence(ctx, models.SourcePostFileEvidence{SourcePostEvidence: models.SourcePostEvidence{
					UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "migration", Basis: "catalog-appearance", ObservedAt: observation.ObservedAt}, ObservationUUID: observation.UUID})
				return err
			}))
			media := linkAlbumPostMedia(t, f.repo, post.UUID, models.ArchiveImage, 41)
			switch choice {
			case "disabled":
				applySelection(t, f.repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, f.repo, post.UUID).Revision, Mode: "disabled", Origin: "review"})
			case "unlinked":
				_, err := applyPostMedia(f.repo, postMediaInput(t, f.repo, post.UUID, media.UUID, "unlinked"))
				require.NoError(t, err)
			}
			preview, err := f.db.PreviewLegacyTwitterRecovery(t.Context(), input)
			if choice != "different tweet" {
				require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
				return
			}
			require.NoError(t, err)
			require.Empty(t, preview.Fragments)
			created, err := f.db.ApplyLegacyTwitterRecovery(t.Context(), input, preview.Signature)
			require.NoError(t, err)
			require.NotEqual(t, post.UUID, created)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				identity, err := f.repo.SourceEvidence.PostIdentity(ctx, post.UUID)
				require.NoError(t, err)
				require.Equal(t, post.UUID, identity.CanonicalUUID)
				return nil
			}))
		})
	}
}

func TestLegacyTwitterRecoveryRetainsPreviouslyRecoveredUnorderedGallery(t *testing.T) {
	f, input := legacyRecoveryFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	parent := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "album"}, "")
	recordSourceTestCapture(t, f.repo, sourceTestCapture(t, parent.UUID, 1, "profile"))
	linkAlbumPostMedia(t, f.repo, parent.UUID, models.ArchiveImage, 41)
	linkAlbumPostMedia(t, f.repo, parent.UUID, models.ArchiveScene, 31)
	recordPath := func(post, name string) {
		observation := f.observation
		observation.UUID, observation.RelativePath = uuid.NewString(), "example, twitter/"+name
		recordFileObservation(t, f.repo, observation)
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.SourceFile.RecordPostEvidence(ctx, models.SourcePostFileEvidence{SourcePostEvidence: models.SourcePostEvidence{
				UUID: uuid.NewString(), PostUUID: post, Origin: "migration", Basis: "catalog-appearance", ObservedAt: observation.ObservedAt}, ObservationUUID: observation.UUID})
			return err
		}))
	}
	recordPath(parent.UUID, recoveryTweet+"_1.jpg")
	recordPath(parent.UUID, recoveryTweet+"_3.mkv")
	initial := applyAlbumBackfill(t, f.repo, albumBackfillPreview(t, f.repo, parent.UUID, models.SourceAlbumLinkedMediaV1))
	before := filenameSelectionGet(t, f.repo, parent.UUID)
	attachmentSQL(t, f.db, `INSERT INTO images(id,title,created_at,updated_at) VALUES(42,'extra',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(23,1,'1575550205214134278_2.jpg',1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO images_files(image_id,file_id,"primary") VALUES(42,23,1);`)
	fragment := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "extra"}, "")
	recordSourceTestCapture(t, f.repo, sourceTestCapture(t, fragment.UUID, 1, "profile"))
	linkAlbumPostMedia(t, f.repo, fragment.UUID, models.ArchiveImage, 42)
	recordPath(fragment.UUID, recoveryTweet+"_2.jpg")
	input.Evidence = append(input.Evidence, sqlite.LegacyTwitterFile{FileUUID: archiveFind(t, f.repo, models.ArchiveFile, 23).UUID,
		MediaUUID: archiveFind(t, f.repo, models.ArchiveImage, 42).UUID, Generation: 1})
	preview, err := f.db.PreviewLegacyTwitterRecovery(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, parent.UUID, preview.PostUUID)
	post, err := f.db.ApplyLegacyTwitterRecovery(t.Context(), input, preview.Signature)
	require.NoError(t, err)
	require.Equal(t, parent.UUID, post)
	after := filenameSelectionGet(t, f.repo, post)
	require.Equal(t, before.Decision.Mode, after.Decision.Mode)
	require.Equal(t, before.Decision.ManifestUUIDs, after.Decision.ManifestUUIDs)
	require.Equal(t, before.DeclaredAlbum, after.DeclaredAlbum)
	require.Equal(t, before.Entries, after.Entries)
	updated := applyAlbumBackfill(t, f.repo, albumBackfillPreview(t, f.repo, post, models.SourceAlbumIdentifiersV1))
	require.Equal(t, initial.Gallery.GalleryID, updated.Gallery.GalleryID)
	sourceGalleryMemberships(t, f.repo, *updated.Gallery.GalleryID, []int{41, 42}, []int{31})
	backup := filepath.Join(t.TempDir(), "existing-gallery.sqlite")
	require.NoError(t, f.db.Backup(backup))
	require.NoError(t, f.db.AuditForTesting(backup))
}
