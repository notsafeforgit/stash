package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func selectionPost(t *testing.T, repo models.Repository, id string) *models.SourcePost {
	t.Helper()
	var post *models.SourcePost
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.FindPost(ctx, id)
		return err
	}))
	return post
}

func selectionCapture(t *testing.T, repo models.Repository, post string, input models.SourceAttachmentManifestInput) string {
	t.Helper()
	capture := recordSourceTestCapture(t, repo, sourceTestCapture(t, post, 1, "biography"))
	input.CaptureUUID = capture.UUID
	recordAttachmentManifest(t, repo, input)
	return capture.UUID
}

func applySelection(t *testing.T, repo models.Repository, input models.AttachmentSelectionInput) *models.AttachmentSelection {
	t.Helper()
	var ret *models.AttachmentSelection
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAttachment.DecideSelection(ctx, input)
		return err
	}))
	return ret
}

func previewSelection(t *testing.T, repo models.Repository, post, capture string) *models.AttachmentSelectionPreview {
	t.Helper()
	var ret *models.AttachmentSelectionPreview
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAttachment.PreviewSelection(ctx, post, capture)
		return err
	}))
	return ret
}

func TestAttachmentSelectionCombinesSourcesAndSurvivesRestart(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "album"}, "")
	count := 3
	first := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{ExpectedCount: &count,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "first"), sourceAttachmentEntry(2, "third")}})
	preview := previewSelection(t, repo, post.UUID, first)
	require.Nil(t, preview.Current)
	require.True(t, preview.Changed)
	require.True(t, preview.Proposed.IsAlbum())
	input := models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: preview.PostRevision, CaptureUUID: first, Mode: "automatic", Origin: "ingest"}
	selected := applySelection(t, repo, input)
	require.Len(t, selected.Entries, 2)
	second := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(1, "second")}})
	preview = previewSelection(t, repo, post.UUID, second)
	require.Equal(t, selected, preview.Current)
	require.Empty(t, preview.Conflicts)
	require.Len(t, preview.Proposed.Entries, 3)
	require.False(t, preview.Proposed.Complete)
	input.ExpectedPostRevision, input.CaptureUUID = preview.PostRevision, second
	selected = applySelection(t, repo, input)
	require.Len(t, selected.Decision.ManifestUUIDs, 2)
	require.Equal(t, "first", selected.Entries[0].Attachment.Reference.Value)
	require.Equal(t, "second", selected.Entries[1].Attachment.Reference.Value)
	require.Equal(t, "third", selected.Entries[2].Attachment.Reference.Value)
	full := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "first"), sourceAttachmentEntry(1, "second"), sourceAttachmentEntry(2, "third")}})
	preview = previewSelection(t, repo, post.UUID, full)
	require.True(t, preview.Proposed.Complete)
	require.Len(t, preview.Proposed.Decision.ManifestUUIDs, 1)
	input.ExpectedPostRevision, input.CaptureUUID = preview.PostRevision, full
	selected = applySelection(t, repo, input)
	require.True(t, selected.Complete)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	preview = previewSelection(t, repo, post.UUID, full)
	require.False(t, preview.Changed)
	require.Equal(t, selected, preview.Current)
	input.ExpectedPostRevision = preview.PostRevision
	require.Equal(t, selected, applySelection(t, repo, input))
	require.Equal(t, preview.PostRevision, selectionPost(t, repo, post.UUID).Revision, "equivalent evidence must not advance the choice revision")
	// An older partial delivery cannot remove the completed album's other items.
	input.CaptureUUID = first
	require.Equal(t, selected, applySelection(t, repo, input))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(3), queryUint(t, raw, "SELECT count(*) FROM post_attachment_decisions"))
	require.Equal(t, uint(4), queryUint(t, raw, "SELECT count(*) FROM post_attachment_decision_manifests"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM galleries"))
}

func TestAttachmentSelectionConflictsPinnedDisabledAndStaleReview(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "album"}, "")
	first := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "first"), sourceAttachmentEntry(1, "second")}})
	input := models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision,
		CaptureUUID: first, Mode: "automatic", Origin: "ingest"}
	initial := applySelection(t, repo, input)
	reordered := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "second"), sourceAttachmentEntry(1, "first")}})
	preview := previewSelection(t, repo, post.UUID, reordered)
	require.Len(t, preview.Conflicts, 2)
	require.Nil(t, preview.Proposed)
	apply := func() error {
		return repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceAttachment.DecideSelection(ctx, input)
			return err
		})
	}
	input.CaptureUUID = reordered
	require.ErrorIs(t, apply(), models.ErrAttachmentSelectionConflict)
	input.ExpectedPostRevision = preview.PostRevision
	require.ErrorIs(t, apply(), models.ErrAttachmentManifestConflict)
	require.Equal(t, initial, previewSelection(t, repo, post.UUID, first).Current)
	input.Mode, input.Origin, input.Reason = "pinned", "review", "Use the reviewed order"
	pinned := applySelection(t, repo, input)
	require.Equal(t, "second", pinned.Entries[0].Attachment.Reference.Value)
	input.ExpectedPostRevision = selectionPost(t, repo, post.UUID).Revision
	input.Mode, input.Origin, input.CaptureUUID = "automatic", "ingest", reordered
	require.True(t, previewSelection(t, repo, post.UUID, reordered).Protected)
	for _, origin := range []string{"ingest", "migration"} {
		input.Origin = origin
		require.ErrorIs(t, apply(), models.ErrAttachmentSelectionProtected)
	}
	input.Mode, input.Origin, input.CaptureUUID = "disabled", "review", ""
	disabled := applySelection(t, repo, input)
	require.Empty(t, disabled.Entries)
	require.False(t, disabled.IsAlbum())
	input.ExpectedPostRevision = selectionPost(t, repo, post.UUID).Revision
	input.Mode, input.Origin, input.CaptureUUID = "automatic", "ingest", first
	for _, origin := range []string{"ingest", "migration"} {
		input.Origin = origin
		require.ErrorIs(t, apply(), models.ErrAttachmentSelectionProtected)
	}
	input.Origin = "review"
	reenabled := applySelection(t, repo, input)
	require.Equal(t, "first", reenabled.Entries[0].Attachment.Reference.Value)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		history, err := repo.SourceAttachment.SelectionHistory(ctx, post.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 4)
		require.Equal(t, initial.Decision, history[0])
		require.Equal(t, pinned.Decision, history[1])
		require.Equal(t, disabled.Decision, history[2])
		page, err := repo.SourceAttachment.SelectionHistory(ctx, post.UUID, history[2].Revision, 1)
		require.NoError(t, err)
		require.Equal(t, history[3:], page)
		return nil
	}))
}

func TestAttachmentSelectionRollbackScopeAndIntegrityGuards(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture, attachment := attachmentFixture(t, repo)
	otherCapture, _ := attachmentFixture(t, repo)
	post := selectionPost(t, repo, attachment.PostUUID)
	input := models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: post.Revision, CaptureUUID: otherCapture.UUID, Mode: "pinned", Origin: "review"}
	apply := func() error {
		return repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceAttachment.DecideSelection(ctx, input)
			return err
		})
	}
	require.ErrorIs(t, apply(), models.ErrAttachmentSelectionConflict)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("CREATE TRIGGER reject_selection BEFORE INSERT ON post_attachment_selections BEGIN SELECT RAISE(ABORT, 'head failure'); END")
	require.NoError(t, err)
	input.CaptureUUID = capture.UUID
	require.ErrorContains(t, apply(), "head failure")
	require.Equal(t, post, selectionPost(t, repo, post.UUID))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM post_attachment_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM post_attachment_decision_manifests"))
	_, err = raw.Exec("DROP TRIGGER reject_selection")
	require.NoError(t, err)
	selected := applySelection(t, repo, input)
	input.ExpectedPostRevision++
	input.Mode, input.CaptureUUID = "disabled", ""
	disabled := applySelection(t, repo, input)
	_, err = raw.Exec("UPDATE post_attachment_selections SET decision_uuid=? WHERE post_uuid=?", selected.Decision.UUID, post.UUID)
	require.ErrorContains(t, err, "backwards")
	_, err = raw.Exec("UPDATE post_attachment_decisions SET reason='changed' WHERE uuid=?", disabled.Decision.UUID)
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("INSERT INTO post_attachment_decision_manifests(post_uuid, decision_uuid, manifest_uuid) SELECT ?, ?, manifest_uuid FROM source_capture_attachment_manifests WHERE capture_uuid=?", post.UUID, disabled.Decision.UUID, otherCapture.UUID)
	require.ErrorContains(t, err, "FOREIGN KEY")
	input.ExpectedPostRevision++
	input.Mode, input.CaptureUUID = "pinned", capture.UUID
	selected = applySelection(t, repo, input)
	_, err = raw.Exec("DELETE FROM post_attachment_decision_manifests WHERE decision_uuid=?", selected.Decision.UUID)
	require.NoError(t, err)
	err = repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAttachment.Selection(ctx, post.UUID)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.NoError(t, err)
	input.ExpectedPostRevision++
	require.ErrorIs(t, apply(), models.ErrSourcePostForgotten)
}

func TestAttachmentSelectionMigrationPreservesAttachments(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-selections.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+8; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	post, attachment := uuid.NewString(), uuid.NewString()
	_, err = raw.Exec("INSERT INTO source_posts(uuid) VALUES (?)", post)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO source_attachments(uuid, post_uuid, namespace, value, revision) VALUES (?, ?, 'native:reddit', 'OpaqueExistingID', 9)", attachment, post)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	stored := findAttachment(t, db.Repository(), attachment)
	require.Equal(t, 9, stored.Revision)
	require.Equal(t, "OpaqueExistingID", stored.Reference.Value)
	repo := db.Repository()
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		selected, err := repo.SourceAttachment.Selection(ctx, post)
		require.NoError(t, err)
		require.Nil(t, selected, "migration must not invent a selection")
		return nil
	}))
}
