package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestSourceGalleryClaimQueryUsesScopedGalleryIndex(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE post_gallery_links(post_uuid TEXT PRIMARY KEY,decision_uuid TEXT,gallery_uuid TEXT) WITHOUT ROWID;
 CREATE UNIQUE INDEX post_gallery_links_gallery ON post_gallery_links(gallery_uuid) WHERE gallery_uuid IS NOT NULL;
 INSERT INTO post_gallery_links VALUES('owner','d1','old'),('unrelated','d2','unrelated'),('disabled','d3',NULL);`)
	require.NoError(t, err)
	query, args := sourceGalleryClaimQuery([]string{"old", "current"}, "owner")
	var claimed bool
	require.NoError(t, db.Get(&claimed, query, args...))
	require.False(t, claimed)
	query, args = sourceGalleryClaimQuery([]string{"old", "current"}, "new-owner")
	require.NoError(t, db.Get(&claimed, query, args...))
	require.True(t, claimed)
	query, args = sourceGalleryClaimQuery([]string{"free"}, "new-owner")
	require.NoError(t, db.Get(&claimed, query, args...))
	require.False(t, claimed)
	var plans []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+query, args...))
	require.Contains(t, fmt.Sprint(plans), "SEARCH post_gallery_links USING COVERING INDEX post_gallery_links_gallery (gallery_uuid=?)")
	require.NotContains(t, fmt.Sprint(plans), "SCAN post_gallery_links")
}
