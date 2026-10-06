package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestPostMediaBackfillDiscoveryUsesPostCursor(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE source_media_evidence(uuid TEXT PRIMARY KEY,post_uuid TEXT,attachment_uuid TEXT,media_uuid TEXT);
CREATE INDEX source_media_evidence_post ON source_media_evidence(post_uuid,uuid);
CREATE INDEX source_media_evidence_candidates ON source_media_evidence(attachment_uuid,media_uuid)`)
	require.NoError(t, err)
	var expected []string
	for i := 1; i <= 200; i++ {
		post := fmt.Sprintf("00000000-0000-4000-8000-%012x", i)
		if i%5 != 0 {
			expected = append(expected, post)
		}
		for j := range 5 {
			var attachment any
			if i%5 == 0 || j == 0 {
				attachment = "observed-attachment"
			}
			_, err = db.Exec("INSERT INTO source_media_evidence VALUES(?,?,?,?)", fmt.Sprintf("%d/%d", i, j), post, attachment, "media")
			require.NoError(t, err)
		}
	}
	_, err = db.Exec("ANALYZE")
	require.NoError(t, err)
	tx, err := db.Beginx()
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	ctx := context.WithValue(t.Context(), txnKey, tx)
	var actual []string
	after := ""
	for {
		var plans []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, tx.Select(&plans, "EXPLAIN QUERY PLAN "+sourcePostMediaBackfillPostsQuery, after, 17))
		require.Contains(t, fmt.Sprint(plans), "SEARCH source_media_evidence USING INDEX source_media_evidence_post (post_uuid>?)")
		require.NotContains(t, fmt.Sprint(plans), "TEMP B-TREE")
		page, err := (&SourcePostMediaStore{}).BackfillPosts(ctx, after, 17)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), 17)
		actual = append(actual, page...)
		if len(page) < 17 {
			break
		}
		after = page[len(page)-1]
	}
	require.Equal(t, expected, actual, "Each evidenced post appears once; attachment-only posts are excluded")
}
