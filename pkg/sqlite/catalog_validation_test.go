package sqlite

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func TestCatalogValidationUsesIndexedParentsWithStaleStatistics(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "validation.sqlite")
	db := NewDatabase()
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	conn, err := sqlx.Open(sqlite3Driver, path)
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	_, err = conn.Exec("ANALYZE")
	require.NoError(t, err)

	for _, tc := range []struct{ family, query string }{
		{"publisher", catalogPublisherValidationQuery},
		{"attachment", catalogAttachmentValidationQuery},
		{"media", catalogMediaValidationQuery},
		{"document", catalogDocumentValidationQuery},
	} {
		t.Run(tc.family, func(t *testing.T) {
			parent := "catalog_" + tc.family + "_imports"
			receipts := "catalog_" + tc.family + "_records"
			parentIndex := "sqlite_autoindex_" + parent + "_1"
			// Simulate an early ANALYZE followed by many imported catalogs. This
			// estimate made startup scan all parents again for every receipt in
			// the library-scale rehearsal. Statistics are deliberately stale;
			// validation cannot write updated ones before trusting the database.
			_, err := conn.Exec("DELETE FROM sqlite_stat1 WHERE tbl IN (?,?)", parent, receipts)
			require.NoError(t, err)
			_, err = conn.Exec("INSERT INTO sqlite_stat1(tbl,idx,stat) VALUES(?,?,?),(?,?,?)", parent, parentIndex, "1 1", receipts, "sqlite_autoindex_"+receipts+"_1", "800000 500 1")
			require.NoError(t, err)
			_, err = conn.Exec("ANALYZE sqlite_schema")
			require.NoError(t, err)
			var plan []struct {
				ID, Parent, Notused int
				Detail              string
			}
			require.NoError(t, conn.Select(&plan, "EXPLAIN QUERY PLAN "+tc.query))
			indexed := false
			for _, step := range plan {
				require.NotContains(t, step.Detail, "SCAN i LEFT-JOIN", "receipt validation must not scan the checkpoint table per receipt")
				if strings.Contains(step.Detail, "SEARCH i USING INDEX "+parentIndex+" (snapshot_uuid=?) LEFT-JOIN") {
					indexed = true
				}
			}
			require.True(t, indexed, "missing unique parent lookup: %+v", plan)
			var invalid bool
			require.NoError(t, conn.Get(&invalid, tc.query))
			require.False(t, invalid, "an empty native archive must remain valid")
		})
	}
}
