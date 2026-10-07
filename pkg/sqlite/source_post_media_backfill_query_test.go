package sqlite

import (
	"context"
	"fmt"
	"slices"
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
CREATE INDEX source_media_evidence_candidates ON source_media_evidence(attachment_uuid,media_uuid);
CREATE TABLE source_post_identities(post_uuid TEXT PRIMARY KEY,canonical_uuid TEXT NOT NULL);
CREATE INDEX source_post_identities_canonical ON source_post_identities(canonical_uuid,post_uuid)`)
	require.NoError(t, err)
	var expected []string
	for i := 1; i <= 200; i++ {
		post := fmt.Sprintf("00000000-0000-4000-8000-%012x", i)
		_, err = db.Exec("INSERT INTO source_post_identities VALUES(?,?)", post, post)
		require.NoError(t, err)
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
	for _, pair := range [][2]int{{1, 21}, {11, 21}, {199, 200}} {
		original, current := fmt.Sprintf("00000000-0000-4000-8000-%012x", pair[0]), fmt.Sprintf("00000000-0000-4000-8000-%012x", pair[1])
		_, err = db.Exec("UPDATE source_post_identities SET canonical_uuid=? WHERE post_uuid=?", current, original)
		require.NoError(t, err)
		expected = slices.DeleteFunc(expected, func(post string) bool { return post == original })
		if !slices.Contains(expected, current) {
			expected = append(expected, current)
		}
	}
	slices.Sort(expected)
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
		require.Contains(t, fmt.Sprint(plans), "SEARCH root USING COVERING INDEX source_post_identities_canonical (canonical_uuid>?)")
		require.Contains(t, fmt.Sprint(plans), "SEARCH member USING COVERING INDEX source_post_identities_canonical (canonical_uuid=?)")
		require.Contains(t, fmt.Sprint(plans), "SEARCH e USING INDEX source_media_evidence_post (post_uuid=?)")
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
