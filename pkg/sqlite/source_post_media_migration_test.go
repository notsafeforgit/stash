package sqlite_test

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removePostMediaDecisionSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removePostMediaBackfillSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE metadata_decision_post_media; DROP TABLE post_media_supersessions; DROP TABLE post_media_links; DROP TABLE post_media_decisions;
DELETE FROM native_migration_history WHERE version=1000081;`)
	require.NoError(t, err)
}

func TestPostMediaMigrationPreservesExistingEvidenceWithoutSelectingIt(t *testing.T) {
	for _, collision := range []string{"", "post_media_links"} {
		t.Run(collision, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			capture, attachment := attachmentFixture(t, repo)
			media := chooseAlbumMedia(t, repo, attachment.UUID, models.ArchiveScene, 31)
			recordMediaEvidence(t, repo, models.SourceMediaEvidence{UUID: "00000000-0000-4000-8000-000000000091", PostUUID: capture.PostUUID, MediaUUID: media.UUID, Basis: "legacy", Details: []byte(`{}`)})
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			removePostMediaDecisionSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000080,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"source_posts", "source_captures", "source_media_evidence", "attachment_media_decisions", "attachment_media_links", "scenes", "archive_entities"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision != "" {
				_, err = raw.Exec("CREATE TABLE post_media_links(original TEXT); INSERT INTO post_media_links VALUES('unknown retained table')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			err = db.RunAllMigrations()
			if collision != "" {
				require.Error(t, err)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='post_media_decisions'"), "failed migration is atomic")
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM post_media_links").Scan(&original))
				require.Equal(t, "unknown retained table", original)
			} else {
				require.NoError(t, err)
				require.NoError(t, db.ReInitialise())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM post_media_decisions"))
				require.NoError(t, db.Close())
				require.NoError(t, db.Open(db.DatabasePath()))
				require.Equal(t, "undecided", postMediaAssociation(t, db.Repository(), capture.PostUUID, media.UUID).State)
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}

func TestPostMediaStartupRejectsCorruptDecisionBeforeWriting(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "invalid-media"}, "")
	media := archiveFind(t, repo, models.ArchiveScene, 31)
	d, err := applyPostMedia(repo, postMediaInput(t, repo, post.UUID, media.UUID, "linked"))
	require.NoError(t, err)
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='post_media_decision_immutable'").Scan(&guard))
	_, err = raw.Exec("DROP TRIGGER post_media_decision_immutable; PRAGMA foreign_keys=OFF")
	require.NoError(t, err)
	_, err = raw.Exec("UPDATE post_media_decisions SET media_uuid=? WHERE uuid=?", performer.UUID, d.UUID)
	require.NoError(t, err)
	_, err = raw.Exec(guard)
	require.NoError(t, err)
	before, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, db.Open(db.DatabasePath()), "invalid post media decisions")
	after, err := os.ReadFile(db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
