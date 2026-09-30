package sqlite_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func attachmentSQL(t *testing.T, db *sqlite.Database, query string, args ...interface{}) {
	t.Helper()
	repo := db.Repository()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, query, args)
		return err
	}))
}

func recordMediaEvidence(t *testing.T, repo models.Repository, input models.SourceMediaEvidence) *models.SourceMediaEvidence {
	t.Helper()
	var ret *models.SourceMediaEvidence
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAttachment.RecordMediaEvidence(ctx, input)
		return err
	}))
	return ret
}

func applyMediaChoice(repo models.Repository, input models.AttachmentMediaDecisionInput) error {
	return repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.DecideMedia(ctx, input)
		return err
	})
}

func TestSourceMediaEvidenceRequiresCaptureMembershipAndFileProof(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	otherCapture, _ := attachmentFixture(t, repo)
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	file := archiveFind(t, repo, models.ArchiveFile, 21)
	input := models.SourceMediaEvidence{UUID: uuid.NewString(), AttachmentUUID: attachment.UUID, CaptureUUID: capture.UUID,
		MediaUUID: media.UUID, FileUUID: &file.UUID, Basis: "observed-file", Details: []byte(`{"source_id":98765432109876543210}`)}
	apply := func() error {
		return repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceAttachment.RecordMediaEvidence(ctx, input)
			return err
		})
	}
	require.ErrorContains(t, apply(), "current file association")
	attachmentSQL(t, db, `INSERT INTO scenes_files(scene_id, file_id, "primary") VALUES (31, 21, 1)`)
	input.CaptureUUID = otherCapture.UUID
	require.ErrorContains(t, apply(), "containing this attachment")
	input.CaptureUUID = capture.UUID
	stored := recordMediaEvidence(t, repo, input)
	require.JSONEq(t, string(input.Details), string(stored.Details), "large source IDs must retain every digit")
	current := findAttachment(t, repo, attachment.UUID)
	require.Equal(t, attachment.Revision+1, current.Revision)
	require.Equal(t, stored, recordMediaEvidence(t, repo, input))
	require.Equal(t, current, findAttachment(t, repo, attachment.UUID), "replayed evidence does not invalidate a review")
	input.Details = []byte(`{"source_id":98765432109876543211}`)
	require.ErrorIs(t, apply(), models.ErrSourceMediaEvidenceReplay)
	input.Details = []byte(`{"k":1,"k":2}`)
	require.Error(t, apply())
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		page, err := repo.SourceAttachment.MediaEvidence(ctx, attachment.UUID, "", 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourceMediaEvidence{*stored}, page)
		page, err = repo.SourceAttachment.MediaEvidence(ctx, attachment.UUID, stored.UUID, 1)
		require.NoError(t, err)
		require.Empty(t, page)
		decision, err := repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
		require.NoError(t, err)
		require.Nil(t, decision, "recording evidence does not silently select it")
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("UPDATE source_media_evidence SET details='{}' WHERE uuid=?", stored.UUID)
	require.ErrorContains(t, err, "immutable")
	// Even a direct insertion cannot combine one capture with another's manifest.
	_, err = raw.Exec(`INSERT INTO source_media_evidence(uuid, attachment_uuid, capture_uuid, manifest_uuid, position, media_uuid, file_uuid, basis, details)
SELECT ?, attachment_uuid, ?, manifest_uuid, position, media_uuid, file_uuid, basis, details FROM source_media_evidence WHERE uuid=?`, uuid.NewString(), otherCapture.UUID, stored.UUID)
	require.ErrorContains(t, err, "FOREIGN KEY")
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	_, err = raw.Exec(`INSERT INTO source_media_evidence(uuid, attachment_uuid, capture_uuid, manifest_uuid, position, media_uuid, file_uuid, basis, details)
SELECT ?, attachment_uuid, capture_uuid, manifest_uuid, position, ?, file_uuid, basis, details FROM source_media_evidence WHERE uuid=?`, uuid.NewString(), performer.UUID, stored.UUID)
	require.ErrorContains(t, err, "scene or image")
}

func TestSourceMediaSelectionRequiresUniqueVerifiedEvidenceAndPreservesExplicitUnlink(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	attachmentSQL(t, db, `INSERT INTO scenes_files(scene_id, file_id, "primary") VALUES (31, 21, 1)`)
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	file := archiveFind(t, repo, models.ArchiveFile, 21)
	input := models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision,
		State: "linked", MediaUUID: media.UUID, ExpectedMediaRevision: media.Revision, Origin: "ingest"}
	require.ErrorIs(t, applyMediaChoice(repo, input), models.ErrAmbiguousSourceMedia)
	proof := models.SourceMediaEvidence{UUID: uuid.NewString(), AttachmentUUID: attachment.UUID, CaptureUUID: capture.UUID, MediaUUID: media.UUID, FileUUID: &file.UUID, Basis: "legacy"}
	recordMediaEvidence(t, repo, proof)
	input.ExpectedAttachmentRevision++
	require.ErrorIs(t, applyMediaChoice(repo, input), models.ErrAmbiguousSourceMedia, "legacy correlation alone is not file proof")
	proof.UUID, proof.Basis = uuid.NewString(), "observed-file"
	recordMediaEvidence(t, repo, proof)
	require.ErrorIs(t, applyMediaChoice(repo, input), models.ErrSourceAttachmentConflict, "new evidence invalidates a stale preview")
	input.ExpectedAttachmentRevision++
	input.ExpectedMediaRevision--
	require.ErrorIs(t, applyMediaChoice(repo, input), models.ErrSourceAttachmentConflict)
	input.ExpectedMediaRevision++
	require.NoError(t, applyMediaChoice(repo, input))
	input.ExpectedAttachmentRevision++
	require.ErrorIs(t, applyMediaChoice(repo, input), models.ErrSourceAttachmentConflict, "ingestion cannot replace an existing selection")
	unlink := models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: input.ExpectedAttachmentRevision, State: "unlinked", Origin: "review", Reason: "Not this media"}
	require.NoError(t, applyMediaChoice(repo, unlink))
	proof.UUID = uuid.NewString()
	recordMediaEvidence(t, repo, proof)
	input.ExpectedAttachmentRevision = findAttachment(t, repo, attachment.UUID).Revision
	require.ErrorIs(t, applyMediaChoice(repo, input), models.ErrSourceAttachmentConflict, "later observations cannot reverse an explicit unlink")
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		choice, err := repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
		require.NoError(t, err)
		require.Equal(t, "unlinked", choice.State)
		history, err := repo.SourceAttachment.MediaDecisionHistory(ctx, attachment.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 2)
		page, err := repo.SourceAttachment.MediaDecisionHistory(ctx, attachment.UUID, history[0].Revision, 1)
		require.NoError(t, err)
		require.Equal(t, history[1:], page)
		return nil
	}))
}

func TestSourceMediaAmbiguityAndMergeAdoptionDeletionPreserveHistory(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	attachmentSQL(t, db, `INSERT INTO scenes_files(scene_id, file_id, "primary") VALUES (31, 21, 1), (32, 21, 1)`)
	one := archiveFind(t, repo, models.ArchiveScene, 31)
	two := archiveFind(t, repo, models.ArchiveScene, 32)
	file := archiveFind(t, repo, models.ArchiveFile, 21)
	proof := models.SourceMediaEvidence{UUID: uuid.NewString(), AttachmentUUID: attachment.UUID, CaptureUUID: capture.UUID, MediaUUID: one.UUID, FileUUID: &file.UUID, Basis: "verified-bytes"}
	recordMediaEvidence(t, repo, proof)
	other := proof
	other.UUID, other.MediaUUID = uuid.NewString(), two.UUID
	recordMediaEvidence(t, repo, other)
	input := models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision + 2,
		State: "linked", MediaUUID: one.UUID, ExpectedMediaRevision: one.Revision, Origin: "ingest"}
	require.ErrorIs(t, applyMediaChoice(repo, input), models.ErrAmbiguousSourceMedia)
	input.Origin = "review"
	require.NoError(t, applyMediaChoice(repo, input), "explicit review can resolve multiple candidates")
	adoptedUUID := uuid.NewString()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, one.UUID, adoptedUUID, one.Revision)
		return err
	}))
	// Old producer IDs still replay after FK adoption; no new evidence is added.
	require.Equal(t, adoptedUUID, recordMediaEvidence(t, repo, proof).MediaUUID)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
			return err
		}
		return repo.Scene.Destroy(ctx, 31)
	}))
	input.ExpectedAttachmentRevision++
	require.ErrorIs(t, applyMediaChoice(repo, input), models.ErrSourceAttachmentConflict, "reviewing a retired target cannot select it")
	input.State, input.MediaUUID, input.ExpectedMediaRevision = "undecided", "", 0
	require.NoError(t, applyMediaChoice(repo, input))
	input.ExpectedAttachmentRevision++
	two = archiveFind(t, repo, models.ArchiveScene, 32)
	input.State, input.MediaUUID, input.ExpectedMediaRevision, input.Origin = "linked", two.UUID, two.Revision, "ingest"
	require.NoError(t, applyMediaChoice(repo, input), "merged candidates resolve to the same active media")
	fileAdopted := uuid.NewString()
	file = archiveFind(t, repo, models.ArchiveFile, 21)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, file.UUID, fileAdopted, file.Revision)
		return err
	}))
	require.Equal(t, fileAdopted, *recordMediaEvidence(t, repo, proof).FileUUID)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Scene.Destroy(ctx, 32) }))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		history, err := repo.SourceAttachment.MediaDecisionHistory(ctx, attachment.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 3)
		require.Equal(t, adoptedUUID, *history[0].MediaUUID)
		resolved, err := repo.ArchiveEntity.Resolve(ctx, *history[0].MediaUUID)
		require.NoError(t, err)
		require.Equal(t, two.UUID, resolved.UUID)
		require.Equal(t, models.ArchiveEntityDeleted, resolved.State)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceMediaDecisionFailureRollsBackAndHeadsCannotRewind(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	_, attachment := attachmentFixture(t, repo)
	_, other := attachmentFixture(t, repo)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	before := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	_, err := raw.Exec("CREATE TRIGGER reject_media_head BEFORE INSERT ON attachment_media_links BEGIN SELECT RAISE(ABORT, 'head failure'); END")
	require.NoError(t, err)
	input := models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "unlinked", Origin: "review"}
	require.ErrorContains(t, applyMediaChoice(repo, input), "head failure")
	require.Equal(t, attachment, findAttachment(t, repo, attachment.UUID))
	require.Equal(t, before, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM attachment_media_decisions"))
	_, err = raw.Exec("DROP TRIGGER reject_media_head")
	require.NoError(t, err)
	require.NoError(t, applyMediaChoice(repo, input))
	input.ExpectedAttachmentRevision++
	input.State = "undecided"
	require.NoError(t, applyMediaChoice(repo, input))
	var oldest string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM attachment_media_decisions ORDER BY revision LIMIT 1").Scan(&oldest))
	_, err = raw.Exec("UPDATE attachment_media_links SET decision_uuid=? WHERE attachment_uuid=?", oldest, attachment.UUID)
	require.ErrorContains(t, err, "backwards")
	_, err = raw.Exec("INSERT INTO attachment_media_links(attachment_uuid, decision_uuid) VALUES (?, ?)", other.UUID, oldest)
	require.ErrorContains(t, err, "FOREIGN KEY")
	_, err = raw.Exec("UPDATE attachment_media_decisions SET reason='rewritten' WHERE uuid=?", oldest)
	require.ErrorContains(t, err, "immutable")
}
