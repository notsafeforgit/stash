package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestSourceAlbumBrowserQueriesStartFromGalleryAndRedirectIndexes(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE archive_entities(uuid TEXT PRIMARY KEY,redirect_to TEXT);
CREATE TABLE source_post_identities(post_uuid TEXT PRIMARY KEY,canonical_uuid TEXT) WITHOUT ROWID;
INSERT INTO source_post_identities VALUES('p1','p1'),('p2','p2'),('p3','p3'),('p4','p4');
CREATE INDEX archive_entities_redirect ON archive_entities(redirect_to) WHERE redirect_to IS NOT NULL;
CREATE TABLE post_gallery_links(post_uuid TEXT PRIMARY KEY,decision_uuid TEXT,gallery_uuid TEXT) WITHOUT ROWID;
CREATE UNIQUE INDEX post_gallery_links_gallery ON post_gallery_links(gallery_uuid) WHERE gallery_uuid IS NOT NULL;
INSERT INTO archive_entities VALUES('current',NULL),('old','current'),('unrelated',NULL);
INSERT INTO post_gallery_links VALUES('p1','d1','current'),('p2','d2','old'),('p3','d3','unrelated'),('p4','d4',NULL);`)
	require.NoError(t, err)
	var ids []string
	require.NoError(t, db.Select(&ids, sourceAlbumGalleryAliasesQuery, "current"))
	require.Equal(t, []string{"current", "old"}, ids)
	query, args := sourceAlbumGalleryPostsQuery(ids, "", 25)
	var posts []string
	require.NoError(t, db.Select(&posts, query, args...))
	require.Equal(t, []string{"p1", "p2"}, posts)
	query, args = sourceAlbumGalleryPostsQuery(ids, "p1", 1)
	require.NoError(t, db.Select(&posts, query, args...))
	require.Equal(t, []string{"p2"}, posts)
	var plans []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+query, args...))
	require.Contains(t, fmt.Sprint(plans), "SEARCH l USING COVERING INDEX post_gallery_links_gallery (gallery_uuid=?)")
	require.Contains(t, fmt.Sprint(plans), "SEARCH i USING PRIMARY KEY (post_uuid=?)")
	_, err = db.Exec("UPDATE source_post_identities SET canonical_uuid='p2' WHERE post_uuid='p1'")
	require.NoError(t, err)
	query, args = sourceAlbumGalleryPostsQuery(ids, "", 25)
	require.NoError(t, db.Select(&posts, query, args...))
	require.Equal(t, []string{"p2"}, posts, "one canonical post appears once through merged gallery aliases")
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+sourceAlbumGalleryAliasesQuery, "current"))
	require.Contains(t, fmt.Sprint(plans), "SEARCH e USING INDEX archive_entities_redirect (redirect_to=?)")
}
