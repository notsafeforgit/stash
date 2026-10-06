package sqlite_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removePostIdentitySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removePostSelectionProvenanceSchema(t, raw)
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='source_post_identities')").Scan(&exists))
	if !exists {
		return
	}
	_, err := raw.Exec(`DROP TRIGGER source_post_consolidation_scope;
DROP TRIGGER source_post_consolidation_publish;
DROP TRIGGER source_post_consolidation_immutable;
DROP TRIGGER source_post_root_initial;
DROP TRIGGER source_post_root_created;
DROP TRIGGER source_post_root_update;
DROP TRIGGER source_post_uuid_immutable;
DROP TRIGGER source_post_group_identifier;
DROP TRIGGER source_post_group_forgotten;
DROP TABLE source_post_consolidation_context;
DROP TABLE source_post_consolidations;
DROP TABLE source_post_identities;
DELETE FROM native_migration_history WHERE version=1000088;`)
	require.NoError(t, err)
}

func TestPostIdentityMigrationPreservesOriginalEvidenceAndRejectsUnknownTables(t *testing.T) {
	for _, collision := range []string{"", "source_post_identities", "source_post_consolidation_context"} {
		t.Run(collision, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			capture, attachment := attachmentFixture(t, repo)
			chooseAlbumMedia(t, repo, attachment.UUID, models.ArchiveScene, 31)
			forgotten := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "forgotten"}, "")
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			removePostIdentitySchema(t, raw)
			_, err := raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", forgotten.UUID)
			require.NoError(t, err)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000087,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"source_posts", "source_post_identifiers", "source_post_revisions", "source_captures", "source_attachments", "source_attachment_manifests", "source_attachment_entries", "attachment_media_decisions", "attachment_media_links", "scenes", "archive_entities"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision != "" {
				_, err = raw.Exec("CREATE TABLE " + collision + "(original TEXT); INSERT INTO " + collision + " VALUES('unknown retained table')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			err = db.RunAllMigrations()
			if collision != "" {
				require.Error(t, err)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='source_post_consolidations'"), "failed migration is atomic")
				if collision != "source_post_identities" {
					require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='source_post_identities'"))
				}
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM "+collision).Scan(&original))
				require.Equal(t, "unknown retained table", original)
			} else {
				require.NoError(t, err)
				require.NoError(t, db.ReInitialise())
				require.NoError(t, db.Close())
				require.NoError(t, db.Open(db.DatabasePath()))
				require.Equal(t, queryUint(t, raw, "SELECT count(*) FROM source_posts"), queryUint(t, raw, "SELECT count(*) FROM source_post_identities"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_identities WHERE post_uuid!=canonical_uuid"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_consolidations"))
				var canonical string
				require.NoError(t, raw.QueryRow("SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?", capture.PostUUID).Scan(&canonical))
				require.Equal(t, capture.PostUUID, canonical)
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
