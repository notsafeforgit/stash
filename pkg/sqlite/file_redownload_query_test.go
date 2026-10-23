package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestRedownloadReceiptsUseContentAndRemovedGenerationIndexes(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE file_content_versions(file_uuid TEXT,generation INTEGER,content_uuid TEXT);
CREATE INDEX file_content_versions_content ON file_content_versions(content_uuid,file_uuid,generation);
CREATE TABLE file_deduplications(kept_file_uuid TEXT,removed_file_uuid TEXT,removed_generation INTEGER,media_uuid TEXT,root_uuid TEXT,remove_path TEXT,proof TEXT,UNIQUE(removed_file_uuid,removed_generation));`)
	require.NoError(t, err)
	var plans []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+redownloadReceiptsQuery, "content", "root", "path", "/media"))
	require.Contains(t, fmt.Sprint(plans), "SEARCH v USING COVERING INDEX file_content_versions_content (content_uuid=?)")
	require.Contains(t, fmt.Sprint(plans), "SEARCH d USING INDEX sqlite_autoindex_file_deduplications_1 (removed_file_uuid=? AND removed_generation=?)")
}
