package sqlite_test

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeCanonicalPostBackfillSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removePostConsolidationReviewSchema(t, raw)
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000091)").Scan(&exists))
	if !exists {
		return
	}
	body, err := os.ReadFile("migrations/1000082_post_media_backfills.up.sql")
	require.NoError(t, err)
	_, original, found := strings.Cut(string(body), "CREATE TRIGGER post_media_decision_evidence_scope")
	require.True(t, found)
	original, _, found = strings.Cut(original, "\nINSERT INTO native_migration_history")
	require.True(t, found)
	_, err = raw.Exec("DROP TRIGGER post_media_decision_evidence_scope; CREATE TRIGGER post_media_decision_evidence_scope" + original + "; DELETE FROM native_migration_history WHERE version=1000091;")
	require.NoError(t, err)
}

func TestCanonicalPostBackfillMigrationPreservesOriginalRequestsAndFileProof(t *testing.T) {
	f, post, _ := postBackfillFixture(t)
	input := postBackfillRequest(previewPostBackfill(t, f.repo, post))
	result, err := applyPostBackfill(f.repo, input)
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	removeCanonicalPostBackfillSchema(t, raw)
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000090,dirty=0")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"post_media_backfills", "post_media_backfill_decisions", "post_media_decision_evidence", "post_media_decisions", "post_media_links", "source_media_evidence", "source_post_file_evidence", "source_file_matches"} {
		before[table] = albumJobRows(t, raw, table)
	}
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replay, err := applyPostBackfill(f.db.Repository(), input)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
