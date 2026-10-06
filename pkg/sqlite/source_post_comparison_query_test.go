package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestSourcePostComparisonQueriesUseSelectedPostIndexes(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE source_post_identifiers(namespace TEXT,value TEXT,post_uuid TEXT,PRIMARY KEY(namespace,value)) WITHOUT ROWID;
CREATE INDEX source_post_identifiers_post ON source_post_identifiers(post_uuid,namespace,value);
CREATE TABLE source_post_urls(uuid TEXT PRIMARY KEY,post_uuid TEXT,url TEXT,UNIQUE(post_uuid,url));
CREATE TABLE source_attachments(uuid TEXT PRIMARY KEY,post_uuid TEXT,namespace TEXT,value TEXT,revision INTEGER,UNIQUE(post_uuid,namespace,value));
CREATE TABLE attachment_media_links(attachment_uuid TEXT PRIMARY KEY,decision_uuid TEXT);
CREATE TABLE attachment_media_decisions(uuid TEXT PRIMARY KEY,revision INTEGER,state TEXT,media_uuid TEXT,origin TEXT,reason TEXT,created_at DATETIME);
CREATE TABLE post_media_links(post_uuid TEXT,media_uuid TEXT,decision_uuid TEXT,PRIMARY KEY(post_uuid,media_uuid)) WITHOUT ROWID;
CREATE TABLE post_media_decisions(uuid TEXT PRIMARY KEY,post_uuid TEXT,media_uuid TEXT);`)
	require.NoError(t, err)
	for _, tc := range []struct{ query, index string }{
		{postComparisonIdentifiersQuery, "SEARCH source_post_identifiers USING COVERING INDEX source_post_identifiers_post (post_uuid=?)"},
		{postComparisonURLsQuery, "SEARCH source_post_urls USING COVERING INDEX sqlite_autoindex_source_post_urls_2 (post_uuid=?)"},
		{postComparisonAttachmentsQuery, "SEARCH a USING INDEX sqlite_autoindex_source_attachments_2 (post_uuid=?)"},
		{postComparisonMediaQuery, "SEARCH l USING PRIMARY KEY (post_uuid=?)"},
	} {
		var plans []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+tc.query, "selected-post", 513))
		plan := fmt.Sprint(plans)
		require.Contains(t, plan, tc.index)
		require.NotContains(t, plan, "SCAN ")
		require.NotContains(t, plan, "TEMP B-TREE")
	}
}
