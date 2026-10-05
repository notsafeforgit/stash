package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataPolicySampleFilesStayWithinSelectedEntityAndFolder(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	attachmentSQL(t, f.db, `UPDATE folders SET path=? WHERE id=1;
INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(33,21,1);`, filepath.Join(f.root.Binding.Path, "inside"))
	scope := models.MetadataPolicySampleScope{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, EntityUUID: f.entity.UUID}
	file := archiveFind(t, f.repo, models.ArchiveFile, 21)
	lookup := func(after string, limit int) ([]models.MetadataPolicySampleFile, error) {
		var result []models.MetadataPolicySampleFile
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = f.repo.MetadataPolicy.SampleFiles(ctx, scope, after, limit)
			return err
		})
		return result, err
	}
	rows, err := lookup("", 1)
	require.NoError(t, err)
	require.Equal(t, []models.MetadataPolicySampleFile{{FileUUID: file.UUID, RelativePath: "inside/video.mp4"}}, rows)
	rows, err = lookup(file.UUID, 1)
	require.NoError(t, err)
	require.Empty(t, rows)
	scope.EntityUUID = archiveFind(t, f.repo, models.ArchiveScene, 32).UUID
	rows, err = lookup("", 25)
	require.NoError(t, err)
	require.Empty(t, rows)
	scope.EntityUUID = f.entity.UUID
	attachmentSQL(t, f.db, `INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at)
VALUES(22,1,'archive.zip',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
UPDATE files SET zip_file_id=22 WHERE id=21;`)
	rows, err = lookup("", 25)
	require.NoError(t, err)
	require.Empty(t, rows, "ZIP members do not represent a physical sample file")
	attachmentSQL(t, f.db, "UPDATE files SET zip_file_id=NULL WHERE id=21")
	attachmentSQL(t, f.db, "UPDATE folders SET path=? WHERE id=1", f.root.Binding.Path+"-other")
	rows, err = lookup("", 25)
	require.NoError(t, err)
	require.Empty(t, rows, "a matching string prefix is not the same directory")
	_, err = lookup("bad-cursor", 25)
	require.ErrorIs(t, err, models.ErrMetadataPolicyInvalid)
	scope.CollectionRevision++
	_, err = lookup("", 25)
	require.ErrorIs(t, err, models.ErrMetadataPolicyConflict)
}

func TestMetadataPolicySampleSourcesFollowCurrentLinksAndPreserveCaptures(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	capture, attachment := attachmentFixture(t, f.repo)
	secondInput := sourceTestCapture(t, capture.PostUUID, 2, "Updated profile")
	second := recordSourceTestCapture(t, f.repo, secondInput)
	// Repeated attachment slots share one selectable capture/attachment pair.
	recordAttachmentManifest(t, f.repo, models.SourceAttachmentManifestInput{CaptureUUID: second.UUID, Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, attachment.Reference.Value), sourceAttachmentEntry(1, attachment.Reference.Value)}})
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for _, c := range []*models.SourceCapture{capture, second} {
			if err := f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, CaptureUUID: c.UUID}); err != nil {
				return err
			}
		}
		return nil
	}))
	scope := models.MetadataPolicySampleScope{CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, EntityUUID: f.entity.UUID}
	lookup := func(after *models.MetadataPolicySourceCursor, limit int) ([]models.MetadataPolicySampleSource, error) {
		var result []models.MetadataPolicySampleSource
		err := f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = f.repo.MetadataPolicy.SampleSources(ctx, scope, after, limit)
			return err
		})
		return result, err
	}
	rows, err := lookup(nil, 25)
	require.NoError(t, err)
	require.Empty(t, rows, "capture membership alone does not identify depicted media")
	current := findAttachment(t, f.repo, attachment.UUID)
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: current.UUID, ExpectedAttachmentRevision: current.Revision,
		State: "linked", MediaUUID: f.entity.UUID, ExpectedMediaRevision: f.entity.Revision, Origin: "review"}))
	rows, err = lookup(nil, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, capture.PostUUID, rows[0].PostUUID)
	require.NotNil(t, rows[0].CapturedAt)
	next, err := lookup(&rows[0].MetadataPolicySourceCursor, 1)
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.NotEqual(t, rows[0].CaptureUUID, next[0].CaptureUUID)
	end, err := lookup(&next[0].MetadataPolicySourceCursor, 25)
	require.NoError(t, err)
	require.Empty(t, end)
	// Adopted UUIDs and scene merges preserve selection without copying sources.
	adopted := uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveEntity.AdoptUUID(ctx, f.entity.UUID, adopted, f.entity.Revision)
		return err
	}))
	scope.EntityUUID = adopted
	rows, err = lookup(nil, 25)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := f.repo.Scene.RedirectMergedIdentities(ctx, []int{33}, 32); err != nil {
			return err
		}
		return f.repo.Scene.Destroy(ctx, 33)
	}))
	scope.EntityUUID = archiveFind(t, f.repo, models.ArchiveScene, 32).UUID
	rows, err = lookup(nil, 25)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	current = findAttachment(t, f.repo, attachment.UUID)
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: current.UUID, ExpectedAttachmentRevision: current.Revision, State: "unlinked", Origin: "review"}))
	rows, err = lookup(nil, 25)
	require.NoError(t, err)
	require.Empty(t, rows, "retained older links must not resurrect an explicit unlink")
	_, err = lookup(&models.MetadataPolicySourceCursor{CaptureUUID: capture.UUID}, 25)
	require.ErrorIs(t, err, models.ErrMetadataPolicyInvalid)
}
