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

func selectionReviewRequest(t *testing.T, repo models.Repository, post, capture, mode string) (models.AttachmentSelectionReviewApplyInput, *models.AttachmentSelectionReviewPreview) {
	t.Helper()
	input := models.AttachmentSelectionReviewInput{PostUUID: post, PostRevision: selectionPost(t, repo, post).Revision,
		CaptureUUID: capture, Mode: mode, Reason: "Reviewed source order"}
	var preview *models.AttachmentSelectionReviewPreview
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		preview, err = repo.SourceAttachment.PreviewSelectionReview(ctx, input)
		return err
	}))
	return models.AttachmentSelectionReviewApplyInput{AttachmentSelectionReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}, preview
}

func applySelectionReview(t *testing.T, repo models.Repository, input models.AttachmentSelectionReviewApplyInput) (*models.AttachmentSelectionReview, bool, error) {
	t.Helper()
	var receipt *models.AttachmentSelectionReview
	var replayed bool
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		receipt, replayed, err = repo.SourceAttachment.ApplySelectionReview(ctx, input)
		return err
	})
	return receipt, replayed, err
}

func TestAttachmentSelectionReviewKeepsRepeatedPositionsAndDoesNotChangeLibrary(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "review"}, "")
	first := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "first"), sourceAttachmentEntry(1, "second")}})
	applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: selectionPost(t, repo, post.UUID).Revision,
		CaptureUUID: first, Mode: "automatic", Origin: "ingest"})
	second := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{DeclaredAlbum: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "second"), sourceAttachmentEntry(9, "second")}})
	request, preview := selectionReviewRequest(t, repo, post.UUID, second, "pinned")
	require.True(t, preview.Changed)
	require.True(t, preview.Current.Complete)
	require.False(t, preview.Proposed.Complete)
	require.True(t, preview.Proposed.DeclaredAlbum)
	require.Len(t, preview.Proposed.Entries, 2)
	require.Equal(t, 9, preview.Proposed.Entries[1].Position)
	require.Equal(t, preview.Proposed.Entries[0].AttachmentUUID, preview.Proposed.Entries[1].AttachmentUUID)
	require.Equal(t, request.PostRevision, selectionPost(t, repo, post.UUID).Revision, "preview must only read")
	before := map[string][][]any{}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, table := range []string{"scenes", "images", "galleries", "post_gallery_links", "attachment_media_links"} {
		before[table] = albumJobRows(t, raw, table)
	}
	receipt, replayed, err := applySelectionReview(t, repo, request)
	require.NoError(t, err)
	require.False(t, replayed)
	require.NotEmpty(t, receipt.DecisionUUID)
	require.Equal(t, request.PostRevision+1, selectionPost(t, repo, post.UUID).Revision)
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	_, same := selectionReviewRequest(t, repo, post.UUID, second, "pinned")
	require.False(t, same.Changed)
	require.Equal(t, receipt.DecisionUUID, same.Current.DecisionUUID)

	// A later explicit disable and restart must not invalidate the old receipt.
	disable, disabled := selectionReviewRequest(t, repo, post.UUID, "", "disabled")
	require.Empty(t, disabled.Proposed.Entries)
	require.NotNil(t, disabled.Proposed.Entries)
	_, _, err = applySelectionReview(t, repo, disable)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	recovered, replayed, err := applySelectionReview(t, repo, request)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, receipt, recovered)
	_, automatic := selectionReviewRequest(t, repo, post.UUID, first, "automatic")
	require.Equal(t, "disabled", automatic.Current.Mode)
	require.Equal(t, "automatic", automatic.Proposed.Mode)
	require.Equal(t, "first", automatic.Proposed.Entries[0].Reference.Value)
	require.Len(t, automatic.Proposed.ManifestUUIDs, 1, "explicit review chooses that capture, without merging the prior list")
}

func TestAttachmentSelectionReviewRejectsStalePreviewAndRequestReuse(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "review"}, "")
	capture := selectionCapture(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "media")}})
	request, _ := selectionReviewRequest(t, repo, post.UUID, capture, "pinned")
	wrong := request
	wrong.Digest = strings.Repeat("0", 64)
	_, _, err := applySelectionReview(t, repo, wrong)
	require.ErrorIs(t, err, models.ErrAttachmentSelectionConflict)
	_, _, err = applySelectionReview(t, repo, request)
	require.NoError(t, err)
	wrong = request
	wrong.RequestUUID = uuid.NewString()
	_, _, err = applySelectionReview(t, repo, wrong)
	require.ErrorIs(t, err, models.ErrAttachmentSelectionConflict)
	wrong = request
	wrong.Reason = "A different request using the old identity"
	_, _, err = applySelectionReview(t, repo, wrong)
	require.ErrorIs(t, err, models.ErrAttachmentSelectionReviewReplay)
	_, _, err = repo.SourceAttachment.ApplySelectionReview(t.Context(), request)
	require.Error(t, err, "even replay uses a managed transaction")
}

func TestAttachmentSelectionReviewRejectsForeignCapturesAndMalformedChoices(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "one"}, "")
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "two"}, "")
	capture := selectionCapture(t, repo, other.UUID, models.SourceAttachmentManifestInput{Complete: true,
		Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "media")}})
	input := models.AttachmentSelectionReviewInput{PostUUID: post.UUID, PostRevision: selectionPost(t, repo, post.UUID).Revision, Mode: "pinned", CaptureUUID: capture}
	for _, item := range []struct {
		input models.AttachmentSelectionReviewInput
		err   error
	}{
		{input, models.ErrAttachmentSelectionConflict},
		{models.AttachmentSelectionReviewInput{PostUUID: post.UUID, PostRevision: input.PostRevision, Mode: "disabled", CaptureUUID: capture}, models.ErrAttachmentSelectionReviewInvalid},
		{models.AttachmentSelectionReviewInput{PostUUID: post.UUID, PostRevision: input.PostRevision, Mode: "pinned"}, models.ErrAttachmentSelectionReviewInvalid},
		{models.AttachmentSelectionReviewInput{PostUUID: post.UUID, PostRevision: 0, Mode: "disabled"}, models.ErrAttachmentSelectionReviewInvalid},
	} {
		err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceAttachment.PreviewSelectionReview(ctx, item.input)
			return err
		})
		require.ErrorIs(t, err, item.err)
	}
}

func TestAttachmentSelectionReviewLateFailureCannotCommitUnreceiptedChoice(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "review"}, "")
	request, _ := selectionReviewRequest(t, repo, post.UUID, "", "disabled")
	attachmentSQL(t, db, `CREATE TRIGGER reject_selection_review BEFORE INSERT ON attachment_selection_reviews BEGIN SELECT RAISE(ABORT,'receipt failure'); END`)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := repo.SourceAttachment.ApplySelectionReview(ctx, request)
		require.ErrorContains(t, err, "receipt failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrAttachmentSelectionReviewInvalid)
	require.Equal(t, request.PostRevision, selectionPost(t, repo, post.UUID).Revision)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		selected, err := repo.SourceAttachment.Selection(ctx, post.UUID)
		require.Nil(t, selected)
		return err
	}))
}

func removeAttachmentSelectionReviewSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec("DROP TABLE attachment_selection_reviews; DELETE FROM native_migration_history WHERE version=1000085")
	require.NoError(t, err)
}

func TestAttachmentSelectionReviewMigrationRetainsSourceChoicesAndUnknownObjects(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "migration", true: "collision"}[collision], func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "review"}, "")
			applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: post.UUID, ExpectedPostRevision: post.Revision, Mode: "disabled", Origin: "review"})
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			before := albumJobRows(t, raw, "post_attachment_decisions")
			removeAttachmentSelectionReviewSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000084,dirty=0")
			require.NoError(t, err)
			if collision {
				_, err = raw.Exec("CREATE TABLE attachment_selection_reviews(original TEXT); INSERT INTO attachment_selection_reviews VALUES('retain unknown data')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			err = db.RunAllMigrations()
			if collision {
				require.Error(t, err)
				var retained string
				require.NoError(t, raw.QueryRow("SELECT original FROM attachment_selection_reviews").Scan(&retained))
				require.Equal(t, "retain unknown data", retained)
			} else {
				require.NoError(t, err)
				require.NoError(t, db.ReInitialise())
				require.NoError(t, db.Close())
				require.NoError(t, db.Open(db.DatabasePath()))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM attachment_selection_reviews"))
			}
			require.Equal(t, before, albumJobRows(t, raw, "post_attachment_decisions"))
		})
	}
}

func TestAttachmentSelectionReviewAnonymisesAndRejectsCorruptReceiptBeforeWrites(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "private-review"}, "")
	request, _ := selectionReviewRequest(t, repo, post.UUID, "", "disabled")
	_, _, err := applySelectionReview(t, repo, request)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(db, path)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	raw := openRawDB(t, path)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM attachment_selection_reviews"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, raw.Close())
	require.NoError(t, db.Close())
	raw = openRawDB(t, db.DatabasePath())
	defer raw.Close()
	var trigger string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='attachment_selection_review_immutable'").Scan(&trigger))
	_, err = raw.Exec("DROP TRIGGER attachment_selection_review_immutable; UPDATE attachment_selection_reviews SET signature='" + strings.Repeat("0", 64) + "'")
	require.NoError(t, err)
	_, err = raw.Exec(trigger)
	require.NoError(t, err)
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(db.DatabasePath()), "source-list selection review")
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
