package sqlite

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stretchr/testify/require"
)

func TestNativeStartupValidationDoesNotReadArchiveHistory(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "startup.sqlite")
	db := NewDatabase()
	require.NoError(t, db.Open(path))
	require.NoError(t, db.Close())
	conn, err := sqlx.Open(sqlite3Driver, path)
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	var plan []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, conn.Select(&plan, "EXPLAIN QUERY PLAN "+unfinishedMetadataCollectionsQuery))
	indexed := false
	for _, step := range plan {
		if strings.Contains(step.Detail, "metadata_field_decisions_unsealed") {
			indexed = true
		}
	}
	require.True(t, indexed, "unfinished decisions must use the bounded partial index: %+v", plan)

	// Deny reads of archive rows at SQLite's authorization boundary. This
	// catches an accidental full scan even with an empty fixture and no timing
	// threshold. Only schema metadata and bounded unfinished-write guards pass.
	allowed := map[string]bool{
		"sqlite_master": true, "sqlite_schema": true, "schema_migrations": true,
		"native_schema": true, "pragma_table_info": true, "pragma_index_list": true,
		"pragma_index_info": true, "pragma_index_xinfo": true,
		"source_account_consolidation_context": true, "capture_publisher_write_context": true,
		"source_post_consolidation_context": true, "source_gallery_write_context": true,
		"metadata_field_write_context": true, "metadata_field_pending": true,
		"metadata_field_decisions": true, // sealed=0 uses its required partial index
	}
	var denied []string
	setAuthorizer := func(callback func(int, string, string, string) int) {
		t.Helper()
		raw, err := conn.Conn(t.Context())
		require.NoError(t, err)
		require.NoError(t, raw.Raw(func(driverConn any) error {
			driverConn.(*CustomSQLiteConn).RegisterAuthorizer(callback)
			return nil
		}))
		require.NoError(t, raw.Close())
	}
	setAuthorizer(func(operation int, table, column, database string) int {
		if operation == sqlite3.SQLITE_READ && !allowed[table] {
			denied = append(denied, table+"."+column)
			return sqlite3.SQLITE_DENY
		}
		return sqlite3.SQLITE_OK
	})
	defer setAuthorizer(nil)
	require.NoError(t, validateNativeConnection(conn, false))
	require.Empty(t, denied)
	require.Error(t, validateNativeConnection(conn, true), "explicit audit must still read archive history")
	require.NotEmpty(t, denied)
}
