package sqlite_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func fileHistoryFixture(t *testing.T) (sourceFileFixture, models.SourceFileHistory) {
	t.Helper()
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	recordFileObservation(t, f.repo, f.observation)
	edits, err := archive.CatalogFileEdits([]byte(`{"title":"Reviewed title","details":null}`))
	require.NoError(t, err)
	input := models.SourceFileHistory{UUID: uuid.NewString(), Kind: "metadata_edit", CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision,
		RootUUID: f.root.UUID, RootRevision: f.root.Revision, ReferenceNamespace: "source:fixture", ReferenceValue: "edit-id", SourceTime: "",
		Origin: "migration", ObservedAt: f.observation.ObservedAt, Edits: edits,
		Locations: []models.SourceFileHistoryLocation{{Position: 0, RelativePath: f.observation.RelativePath, ObservationUUID: &f.observation.UUID}}}
	return f, input
}

func recordFileHistory(t *testing.T, repo models.Repository, input models.SourceFileHistory) *models.SourceFileHistory {
	t.Helper()
	var ret *models.SourceFileHistory
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceFileHistory.Record(ctx, input)
		return err
	}))
	return ret
}

func TestSourceFileHistoryReplayAndAtomicFailure(t *testing.T) {
	f, input := fileHistoryFixture(t)
	stored := recordFileHistory(t, f.repo, input)
	require.Empty(t, stored.SourceTime, "an unknown source clock must not be replaced by the migration time")
	require.Equal(t, stored, recordFileHistory(t, f.repo, input))
	changed := input
	changed.ReferenceValue = "different edit"
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceFileHistory.Record(ctx, changed)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceFileEvidenceReplay)
	// A late child failure must not permit a caller to commit an incomplete event.
	attachmentSQL(t, f.db, `CREATE TRIGGER reject_history_edit BEFORE INSERT ON source_file_history_edits BEGIN SELECT RAISE(ABORT,'child failure'); END`)
	input.UUID = uuid.NewString()
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceFileHistory.Record(ctx, input)
		require.ErrorContains(t, err, "child failure")
		return nil
	})
	require.ErrorIs(t, err, models.ErrSourceFileHistoryInvalid)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_file_history"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_file_history_locations"))
	attachmentSQL(t, f.db, "DROP TRIGGER reject_history_edit")
	input.Locations[0].RelativePath = "another.mp4"
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceFileHistory.Record(ctx, input)
		return err
	})
	require.ErrorContains(t, err, "outside its scope")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestSourceFileHistoryPreparedAndFinishedArchiveMembers(t *testing.T) {
	f, input := fileHistoryFixture(t)
	member := f.observation
	member.UUID, member.RelativePath = uuid.NewString(), "nested/photo.jpg"
	container := "album.zip"
	member.ArchivePath = &container
	recordFileObservation(t, f.repo, member)
	input.Kind, input.ReferenceValue, input.Edits = "deduplication", "operation", nil
	input.Locations = append(input.Locations, models.SourceFileHistoryLocation{Position: 1, RelativePath: member.RelativePath, ArchivePath: &container, ObservationUUID: &member.UUID})
	input.Deduplication = &models.SourceFileDeduplication{ContentClaimUUID: f.claim.UUID, Stage: "prepared"}
	prepared := recordFileHistory(t, f.repo, input)
	require.Nil(t, prepared.Deduplication.SurvivorPath)
	input.UUID = uuid.NewString()
	survivor := "album.zip/nested/photo.jpg"
	input.Deduplication = &models.SourceFileDeduplication{ContentClaimUUID: f.claim.UUID, Stage: "finished", SurvivorPath: &survivor}
	finished := recordFileHistory(t, f.repo, input)
	require.Equal(t, prepared.ReferenceValue, finished.ReferenceValue)
	require.NotEqual(t, prepared.UUID, finished.UUID)
	require.Equal(t, finished, recordFileHistory(t, f.repo, input))
	input.UUID = uuid.NewString()
	input.Locations[1] = input.Locations[0]
	input.Locations[1].Position = 1
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceFileHistory.Record(ctx, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceFileHistoryInvalid)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestSourceFileHistoryStartupRejectsAlteredPayloadOrMissingChildrenWithoutWriting(t *testing.T) {
	for _, scenario := range []string{"value", "clock", "missing-child", "receipt"} {
		t.Run(scenario, func(t *testing.T) {
			f, input := fileHistoryFixture(t)
			recordFileHistory(t, f.repo, input)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			if scenario == "missing-child" {
				_, err := raw.Exec("DELETE FROM source_file_history_edits WHERE field='title'")
				require.NoError(t, err)
			} else {
				trigger, change := "source_file_history_immutable", "UPDATE source_file_history SET source_time='2026-01-02T03:04:05Z'"
				switch scenario {
				case "value":
					trigger, change = "source_file_history_edit_immutable", `UPDATE source_file_history_edits SET value_json='"Altered"' WHERE field='title'`
				case "receipt":
					change = "UPDATE source_file_history SET reference_value='altered'"
				}
				var guard string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", trigger).Scan(&guard))
				_, err := raw.Exec("DROP TRIGGER " + trigger + ";" + change + ";" + guard)
				require.NoError(t, err)
			}
			before, err := os.ReadFile(f.db.DatabasePath())
			require.NoError(t, err)
			require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
			after, err := os.ReadFile(f.db.DatabasePath())
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
