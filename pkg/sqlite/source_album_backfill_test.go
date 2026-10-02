package sqlite_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func albumBackfillPreview(t *testing.T, repo models.Repository, post, policy string) *models.SourceAlbumBackfillPreview {
	t.Helper()
	var ret *models.SourceAlbumBackfillPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.PreviewBackfill(ctx, post, policy)
		return err
	}))
	return ret
}

func applyAlbumBackfill(t *testing.T, repo models.Repository, preview *models.SourceAlbumBackfillPreview) *models.SourceAlbumBackfillResult {
	t.Helper()
	var ret *models.SourceAlbumBackfillResult
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceGallery.Backfill(ctx, preview.PostUUID, preview.Policy, preview.Signature)
		return err
	}))
	return ret
}

func albumEntry(position int, namespace, id, kind string) models.SourceAttachmentEntry {
	return models.SourceAttachmentEntry{Position: position, Reference: models.SourcePostIdentifier{Namespace: namespace, Value: id}, MediaKind: kind}
}

func albumLegacyEvidence(t *testing.T, f sourceFileFixture, post, relative string, sourceID any, kind models.ArchiveEntityKind, mediaID, fileID int) *models.SourceMediaEvidence {
	t.Helper()
	basename := "video.mp4"
	if fileID != 21 {
		basename = fmt.Sprintf("media-%d.mp4", fileID)
		attachmentSQL(t, f.db, `INSERT OR IGNORE INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at)
 VALUES(?,1,?,1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, fileID, basename)
	}
	table, column := "scenes_files", "scene_id"
	if kind == models.ArchiveImage {
		table, column = "images_files", "image_id"
	}
	attachmentSQL(t, f.db, "INSERT OR IGNORE INTO "+table+"("+column+`,file_id,"primary") VALUES(?,?,1)`, mediaID, fileID)
	observation := f.observation
	observation.UUID, observation.RelativePath, observation.State, observation.SurvivorPath = uuid.NewString(), relative, "deduplicated", &basename
	recordFileObservation(t, f.repo, observation)
	file := archiveFind(t, f.repo, models.ArchiveFile, fileID)
	match := recordFileMatch(t, f.repo, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: file.UUID, Generation: 1,
		LibraryRootPath: "/identity-fixture", Basis: "survivor-path", Origin: "migration"})
	details, err := json.Marshal(map[string]any{"source_media_id": sourceID, "position": 999, "source_relpath": relative})
	require.NoError(t, err)
	postFile := models.SourcePostFileEvidence{SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post, Origin: "migration", Basis: "catalog-appearance", ObservedAt: f.observation.ObservedAt, Details: details}, ObservationUUID: observation.UUID}
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceFile.RecordPostEvidence(ctx, postFile)
		return err
	}))
	details, err = json.Marshal(map[string]string{"source_post_file_evidence_uuid": postFile.UUID, "source_file_match_uuid": match.UUID})
	require.NoError(t, err)
	media := archiveFind(t, f.repo, kind, mediaID)
	return recordMediaEvidence(t, f.repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post, MediaUUID: media.UUID, FileUUID: &file.UUID, Basis: "legacy", Details: details})
}

func singleBackfillFixture(t *testing.T) (sourceFileFixture, string, string) {
	t.Helper()
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "ab123"}, "")
	selection := selectAlbum(t, f.repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{albumEntry(0, "native:reddit", "cd456", "video")}})
	albumLegacyEvidence(t, f, post.UUID, "folder/ab123_cd456_Original.gif", nil, models.ArchiveScene, 31, 21)
	return f, post.UUID, selection.Entries[0].Attachment.UUID
}

func TestSourceAlbumBackfillOrdersMixedMediaAndUsesOriginalPaths(t *testing.T) {
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "ab123"}, "")
	selection := selectAlbum(t, f.repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{
		albumEntry(0, "native:reddit", "img123", "image"), albumEntry(2, "native:reddit", "vid123", "video"),
		albumEntry(3, "native:reddit", "img123", "image"), albumEntry(8, "native:reddit", "missing", "image"),
	}})
	albumLegacyEvidence(t, f, post.UUID, "one/ab123_img123_Image.jpg", nil, models.ArchiveImage, 41, 22)
	albumLegacyEvidence(t, f, post.UUID, "two/ab123_img123_Same image.jpg", nil, models.ArchiveImage, 41, 22)
	albumLegacyEvidence(t, f, post.UUID, "one/ab123_vid123_Converted.gif", nil, models.ArchiveScene, 31, 21)
	strict := albumBackfillPreview(t, f.repo, post.UUID, models.SourceAlbumIdentifiersV1)
	require.Empty(t, strict.Gallery.Add)
	preview := albumBackfillPreview(t, f.repo, post.UUID, models.SourceAlbumRedditFilenameV1)
	require.Equal(t, preview, albumBackfillPreview(t, f.repo, post.UUID, preview.Policy), "preview is read only and deterministic")
	require.Len(t, preview.Matches, 3, "repeated attachment slots share one decision")
	require.Equal(t, "matched", preview.Matches[0].Status)
	require.Len(t, preview.Matches[0].Candidates, 1)
	require.Len(t, preview.Matches[0].Candidates[0].Proofs, 2, "copied catalogs retain their evidence without becoming multiple media candidates")
	require.Equal(t, "unavailable", preview.Matches[2].Status)
	require.Equal(t, []int{0, 2, 3, 8}, []int{preview.Gallery.Entries[0].Position, preview.Gallery.Entries[1].Position, preview.Gallery.Entries[2].Position, preview.Gallery.Entries[3].Position})
	require.Len(t, preview.Gallery.Add, 2)
	require.Empty(t, sourceGalleryPreview(t, f.repo, post.UUID).Add)
	result := applyAlbumBackfill(t, f.repo, preview)
	require.True(t, result.Gallery.Created)
	require.Equal(t, 2, result.Selected)
	require.Equal(t, 1, result.Unavailable)
	sourceGalleryMemberships(t, f.repo, *result.Gallery.GalleryID, []int{41}, []int{31})
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_media_evidence WHERE attachment_uuid IS NOT NULL AND capture_uuid IS NULL AND basis='legacy'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM media_contents"), "historical path matches are not fresh byte verification")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM performers_galleries"), "publisher/folder does not assign depicted performers")
	var title string
	require.NoError(t, raw.QueryRow("SELECT title FROM scenes WHERE id=31").Scan(&title))
	require.Equal(t, "Kept title", title)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	fresh := albumBackfillPreview(t, f.repo, post.UUID, preview.Policy)
	require.Equal(t, "preserved", fresh.Matches[0].Status)
	require.Equal(t, "preserved", fresh.Matches[1].Status)
	replay := applyAlbumBackfill(t, f.repo, fresh)
	require.False(t, replay.Gallery.Created)
	require.Zero(t, replay.Selected)
	require.Empty(t, replay.Gallery.Added)
	require.Equal(t, result.Gallery.GalleryUUID, replay.Gallery.GalleryUUID)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		history, err := f.repo.SourceAttachment.MediaDecisionHistory(ctx, selection.Entries[0].Attachment.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 1)
		return nil
	}))
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceGallery.Backfill(ctx, post.UUID, preview.Policy, preview.Signature)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceGalleryConflict)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceAlbumBackfillHonorsNamespacesExplicitIDsAndFilenameBoundaries(t *testing.T) {
	for _, scenario := range []struct {
		name, namespace, path string
		sourceID              any
		want                  string
	}{
		{"reddit explicit", "native:reddit", "unrelated.gif", "reddit:media:cd456", "matched"},
		{"twitter explicit", "native:twitter", "unrelated.gif", "twitter:media:cd456", "matched"},
		{"mirror qualified", "mirror:coomer:onlyfans", "ab123_cd456_title.gif", "reddit:media:cd456", "unavailable"},
		{"conflicting id", "native:reddit", "ab123_cd456_title.gif", "reddit:media:other", "unavailable"},
		{"delegated service", "native:reddit", "ab123_cd456_title.gif", "redgifs:media:cd456", "unavailable"},
		{"malformed id", "native:reddit", "ab123_cd456_title.gif", 123, "unavailable"},
		{"longer media id", "native:reddit", "ab123_cd4567_title.gif", nil, "unavailable"},
		{"other post", "native:reddit", "other_cd456_title.gif", nil, "unavailable"},
		{"no delimiter", "native:reddit", "ab123_cd456", nil, "unavailable"},
		{"exact extension", "native:reddit", "ab123_cd456.gif", nil, "matched"},
		{"twitter filename", "native:twitter", "ab123_cd456_title.gif", nil, "unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newSourceFileFixture(t)
			recordContentClaim(t, f.repo, f.claim)
			post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: scenario.namespace, Value: "ab123"}, "")
			selectAlbum(t, f.repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
				Entries: []models.SourceAttachmentEntry{albumEntry(0, scenario.namespace, "cd456", "video")}})
			albumLegacyEvidence(t, f, post.UUID, scenario.path, scenario.sourceID, models.ArchiveScene, 31, 21)
			preview := albumBackfillPreview(t, f.repo, post.UUID, models.SourceAlbumRedditFilenameV1)
			require.Equal(t, scenario.want, preview.Matches[0].Status)
			if scenario.name == "twitter explicit" || scenario.name == "reddit explicit" {
				require.Equal(t, "matched", albumBackfillPreview(t, f.repo, post.UUID, models.SourceAlbumIdentifiersV1).Matches[0].Status)
			}
		})
	}
}

func TestSourceAlbumBackfillPreservesEveryExistingDecision(t *testing.T) {
	for _, state := range []string{"linked", "unlinked", "undecided"} {
		t.Run(state, func(t *testing.T) {
			f, post, attachment := singleBackfillFixture(t)
			input := models.AttachmentMediaDecisionInput{AttachmentUUID: attachment, ExpectedAttachmentRevision: findAttachment(t, f.repo, attachment).Revision, State: state, Origin: "review"}
			if state == "linked" {
				other := archiveFind(t, f.repo, models.ArchiveScene, 32)
				input.MediaUUID, input.ExpectedMediaRevision = other.UUID, other.Revision
			}
			require.NoError(t, applyMediaChoice(f.repo, input))
			preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumRedditFilenameV1)
			require.Equal(t, "preserved", preview.Matches[0].Status)
			require.NotEmpty(t, preview.Matches[0].DecisionUUID)
			result := applyAlbumBackfill(t, f.repo, preview)
			require.Zero(t, result.Selected)
			if state == "linked" {
				sourceGalleryMemberships(t, f.repo, *result.Gallery.GalleryID, nil, []int{32})
			} else {
				sourceGalleryMemberships(t, f.repo, *result.Gallery.GalleryID, nil, nil)
			}
		})
	}
}

func TestSourceAlbumBackfillRejectsStaleAndAmbiguousEvidence(t *testing.T) {
	for _, change := range []string{"generation", "owner", "deleted", "other-evidence", "new-candidate", "kind", "post", "selection"} {
		t.Run(change, func(t *testing.T) {
			f, post, attachment := singleBackfillFixture(t)
			preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumRedditFilenameV1)
			switch change {
			case "generation":
				attachmentSQL(t, f.db, "UPDATE files SET size=1001 WHERE id=21")
			case "owner":
				attachmentSQL(t, f.db, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(32,21,1)`)
			case "deleted":
				attachmentSQL(t, f.db, "DELETE FROM scenes WHERE id=31")
			case "other-evidence":
				media := archiveFind(t, f.repo, models.ArchiveScene, 32)
				recordMediaEvidence(t, f.repo, models.SourceMediaEvidence{UUID: uuid.NewString(), PostUUID: post, AttachmentUUID: attachment, MediaUUID: media.UUID, Basis: "review"})
			case "new-candidate":
				albumLegacyEvidence(t, f, post, "copy/ab123_cd456_Alternative.mp4", nil, models.ArchiveScene, 32, 23)
			case "kind":
				selectAlbum(t, f.repo, post, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{albumEntry(0, "native:reddit", "cd456", "image")}})
			case "post":
				attachmentSQL(t, f.db, "UPDATE source_posts SET state='forgotten' WHERE uuid=?", post)
			case "selection":
				applySelection(t, f.repo, models.AttachmentSelectionInput{PostUUID: post, ExpectedPostRevision: selectionPost(t, f.repo, post).Revision, Mode: "disabled", Origin: "review"})
			}
			err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.SourceGallery.Backfill(ctx, post, preview.Policy, preview.Signature)
				return err
			})
			require.Error(t, err)
			if change != "post" {
				fresh := albumBackfillPreview(t, f.repo, post, preview.Policy)
				if change != "selection" {
					require.Contains(t, []string{"review", "ambiguous"}, fresh.Matches[0].Status)
				}
				result := applyAlbumBackfill(t, f.repo, fresh)
				require.Zero(t, result.Selected)
				require.Empty(t, result.Gallery.Added)
			}
		})
	}
}

func TestSourceAlbumBackfillRollbackAndPrecommitGuard(t *testing.T) {
	for _, failure := range []string{"write-error", "late-generation", "late-owner", "late-decision"} {
		t.Run(failure, func(t *testing.T) {
			f, post, attachment := singleBackfillFixture(t)
			preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumRedditFilenameV1)
			if failure == "write-error" {
				attachmentSQL(t, f.db, `CREATE TRIGGER backfill_failure BEFORE INSERT ON galleries BEGIN SELECT RAISE(ABORT,'injected gallery failure'); END;`)
			}
			err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := f.repo.SourceGallery.Backfill(ctx, post, preview.Policy, preview.Signature)
				if failure == "write-error" {
					require.ErrorContains(t, err, "injected gallery failure")
					return nil // An inattentive caller still cannot commit partial work.
				}
				require.NoError(t, err)
				switch failure {
				case "late-generation":
					_, _, err = f.db.ExecSQL(ctx, "UPDATE files SET size=1001 WHERE id=21", nil)
				case "late-owner":
					_, _, err = f.db.ExecSQL(ctx, "DELETE FROM scenes_files WHERE file_id=21", nil)
				case "late-decision":
					a, findErr := f.repo.SourceAttachment.Find(ctx, attachment)
					require.NoError(t, findErr)
					_, err = f.repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment, ExpectedAttachmentRevision: a.Revision, State: "unlinked", Origin: "review"})
				}
				return err
			})
			require.Error(t, err)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM galleries"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM attachment_media_decisions"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_media_evidence WHERE attachment_uuid IS NOT NULL"))
			require.Equal(t, preview, albumBackfillPreview(t, f.repo, post, preview.Policy))
		})
	}
}

func TestSourceAlbumBackfillPreservesManualGalleryMembershipAndDeletion(t *testing.T) {
	f, post, _ := singleBackfillFixture(t)
	album := syncSourceGallery(t, f.repo, post)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := f.repo.Gallery.AddSceneIDs(ctx, *album.GalleryID, []int{31, 32}); err != nil {
			return err
		}
		_, err := f.repo.Scene.UpdatePartial(ctx, 31, models.ScenePartial{GalleryIDs: &models.UpdateIDs{Mode: models.RelationshipUpdateModeRemove, IDs: []int{*album.GalleryID}}})
		return err
	}))
	preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumRedditFilenameV1)
	require.Equal(t, "matched", preview.Matches[0].Status, "source-media evidence and manual gallery exclusion are different choices")
	require.Equal(t, "excluded", preview.Gallery.Entries[0].Status)
	require.Empty(t, preview.Gallery.Add)
	result := applyAlbumBackfill(t, f.repo, preview)
	require.Equal(t, 1, result.Selected)
	sourceGalleryMemberships(t, f.repo, *album.GalleryID, nil, []int{32})
	attachmentSQL(t, f.db, "DELETE FROM galleries WHERE id=?", *album.GalleryID)
	preview = albumBackfillPreview(t, f.repo, post, preview.Policy)
	require.Equal(t, "disabled", preview.Gallery.Action)
	result = applyAlbumBackfill(t, f.repo, preview)
	require.False(t, result.Gallery.Created)
	require.Zero(t, result.Selected)
}

func TestSourceAlbumBackfillRequiresPostScopedEvidenceAndCompleteCandidateSet(t *testing.T) {
	f, post, attachment := singleBackfillFixture(t)
	// A different post cannot borrow an observation merely because its file's
	// original basename happens to contain that other post's attachment ID.
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "other"}, "")
	selectAlbum(t, f.repo, other.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true, Entries: []models.SourceAttachmentEntry{albumEntry(0, "native:reddit", "cd456", "video")}})
	var evidence []models.SourceMediaEvidence
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		evidence, err = f.repo.SourceAttachment.PostMediaEvidence(ctx, post, "", 100)
		return err
	}))
	borrowed := evidence[0]
	borrowed.UUID, borrowed.PostUUID = uuid.NewString(), other.UUID
	recordMediaEvidence(t, f.repo, borrowed)
	require.Equal(t, "unavailable", albumBackfillPreview(t, f.repo, other.UUID, models.SourceAlbumRedditFilenameV1).Matches[0].Status)
	preview := albumBackfillPreview(t, f.repo, post, models.SourceAlbumRedditFilenameV1)
	media := archiveFind(t, f.repo, models.ArchiveScene, 31)
	attachmentSQL(t, f.db, `WITH RECURSIVE n(v) AS (VALUES(1) UNION ALL SELECT v+1 FROM n WHERE v<8192)
 INSERT INTO source_media_evidence(uuid,post_uuid,media_uuid,basis,details)
 SELECT printf('00000000-0000-4000-8000-%012d',v),?,?,'review','{}' FROM n`, post, media.UUID)
	require.ErrorIs(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceGallery.PreviewBackfill(ctx, post, preview.Policy)
		return err
	}), models.ErrSourceAlbumLimit)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceGallery.Backfill(ctx, post, preview.Policy, preview.Signature)
		return err
	}), models.ErrSourceAlbumLimit)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		decision, err := f.repo.SourceAttachment.MediaDecision(ctx, attachment)
		require.NoError(t, err)
		require.Nil(t, decision)
		_, err = f.repo.SourceGallery.PreviewBackfill(ctx, post, "unknown-policy")
		require.ErrorIs(t, err, models.ErrSourceAlbumPolicy)
		return nil
	}))
}
