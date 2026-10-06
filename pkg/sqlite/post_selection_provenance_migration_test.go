package sqlite_test

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removePostSelectionProvenanceSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='post_attachment_decision_capture_scope')").Scan(&exists))
	if !exists {
		return
	}
	prior, err := os.ReadFile("migrations/1000009_attachment_selections.up.sql")
	require.NoError(t, err)
	body := string(prior)
	tables, _, found := strings.Cut(body, "CREATE TABLE post_attachment_selections")
	require.True(t, found)
	triggers := body[strings.Index(body, "CREATE TRIGGER post_attachment_decision_immutable"):strings.Index(body, "CREATE TRIGGER post_attachment_selection_forward")]
	triggers += body[strings.Index(body, "CREATE TRIGGER post_attachment_decision_active_post"):strings.Index(body, "INSERT INTO native_migration_history")]
	// Reconstruct the genuine pre-89 foreign keys without replacing referenced
	// decisions, review receipts or gallery histories.
	raw.SetMaxOpenConns(1)
	_, err = raw.Exec("PRAGMA foreign_keys=OFF")
	require.NoError(t, err)
	defer func() { _, err := raw.Exec("PRAGMA foreign_keys=ON"); require.NoError(t, err) }()
	tx, err := raw.Begin()
	require.NoError(t, err)
	defer func() {
		if err := tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
			require.NoError(t, err)
		}
	}()
	_, err = tx.Exec(`CREATE TABLE selection_before89_decisions AS SELECT rowid AS original_rowid,* FROM post_attachment_decisions;
CREATE TABLE selection_before89_manifests AS SELECT * FROM post_attachment_decision_manifests;
DROP TABLE post_attachment_decision_manifests;
DROP TABLE post_attachment_decisions;` + tables + `
INSERT INTO post_attachment_decisions(rowid,uuid,post_uuid,revision,mode,origin,reason,capture_uuid,manifest_count,signature,created_at)
 SELECT original_rowid,uuid,post_uuid,revision,mode,origin,reason,capture_uuid,manifest_count,signature,created_at FROM selection_before89_decisions;
INSERT INTO post_attachment_decision_manifests SELECT * FROM selection_before89_manifests;
DROP TABLE selection_before89_manifests;
DROP TABLE selection_before89_decisions;` + triggers + `DELETE FROM native_migration_history WHERE version=1000089;`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func TestPostSelectionProvenanceMigrationPreservesEvidenceAndRejectsUnknownTables(t *testing.T) {
	for _, collision := range []string{"", "native_selection_decision_rows", "native_selection_manifest_rows"} {
		t.Run(collision, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			capture, _ := attachmentFixture(t, repo)
			applySelection(t, repo, models.AttachmentSelectionInput{PostUUID: capture.PostUUID,
				ExpectedPostRevision: selectionPost(t, repo, capture.PostUUID).Revision, CaptureUUID: capture.UUID, Mode: "pinned", Origin: "review"})
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			removePostSelectionProvenanceSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000088,dirty=0")
			require.NoError(t, err)
			// Retained decisions for forgotten posts must also survive the rebuild.
			_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", capture.PostUUID)
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"source_posts", "source_post_identities", "source_post_revisions", "source_captures", "source_attachments", "source_attachment_manifests", "source_attachment_entries", "post_attachment_decisions", "post_attachment_decision_manifests", "post_attachment_selections"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision != "" {
				_, err = raw.Exec("CREATE TABLE " + collision + "(original TEXT); INSERT INTO " + collision + " VALUES('retained unknown table')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			err = db.RunAllMigrations()
			if collision != "" {
				require.Error(t, err)
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM "+collision).Scan(&original))
				require.Equal(t, "retained unknown table", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='post_attachment_decision_capture_scope'"))
			} else {
				require.NoError(t, err)
				require.NoError(t, db.ReInitialise())
				require.NoError(t, db.Close())
				require.NoError(t, db.Open(db.DatabasePath()))
				require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000089"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
