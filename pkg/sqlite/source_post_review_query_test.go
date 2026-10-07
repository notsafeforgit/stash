package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestSourceMediaPostsQueryStartsFromSelectedMedia(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE source_post_identities(post_uuid TEXT PRIMARY KEY,canonical_uuid TEXT);
CREATE INDEX source_post_identities_canonical ON source_post_identities(canonical_uuid,post_uuid);
CREATE TABLE source_media_evidence(uuid TEXT PRIMARY KEY,post_uuid TEXT,media_uuid TEXT);
CREATE INDEX source_media_evidence_media ON source_media_evidence(media_uuid);
CREATE TABLE post_media_links(post_uuid TEXT,media_uuid TEXT,decision_uuid TEXT,PRIMARY KEY(post_uuid,media_uuid));
CREATE INDEX post_media_links_media ON post_media_links(media_uuid,post_uuid);
CREATE TABLE attachment_media_decisions(uuid TEXT PRIMARY KEY,attachment_uuid TEXT,media_uuid TEXT);
CREATE INDEX attachment_media_decisions_media ON attachment_media_decisions(media_uuid) WHERE media_uuid IS NOT NULL;
CREATE TABLE attachment_media_links(attachment_uuid TEXT PRIMARY KEY,decision_uuid TEXT);
CREATE TABLE source_attachments(uuid TEXT PRIMARY KEY,post_uuid TEXT);`)
	require.NoError(t, err)
	for i := range 5 {
		post := fmt.Sprintf("post-%02d", i)
		_, err = db.Exec("INSERT INTO source_post_identities VALUES(?,?)", post, post)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO source_media_evidence VALUES(?,?,?)", post, post, "media")
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO post_media_links VALUES(?,?,?)", post, "alias", post)
		require.NoError(t, err)
	}
	_, err = db.Exec(`INSERT INTO source_post_identities VALUES('post-05','post-05');
INSERT INTO source_attachments VALUES('attachment','post-05');
INSERT INTO attachment_media_decisions VALUES('decision','attachment','alias');
INSERT INTO attachment_media_links VALUES('attachment','decision');`)
	require.NoError(t, err)
	var result []string
	for after := ""; ; {
		query, args := sourceMediaPostsQuery([]string{"media", "alias"}, after, 2)
		var plans []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+query, args...))
		plan := fmt.Sprint(plans)
		require.Contains(t, plan, "SEARCH e USING INDEX source_media_evidence_media (media_uuid=?)")
		require.Contains(t, plan, "SEARCH l USING COVERING INDEX post_media_links_media (media_uuid=?)")
		require.Contains(t, plan, "SEARCH d USING INDEX attachment_media_decisions_media (media_uuid=?)")
		var page []string
		require.NoError(t, db.Select(&page, query, args...))
		result = append(result, page...)
		if len(page) < 2 {
			break
		}
		after = page[len(page)-1]
	}
	require.Equal(t, []string{"post-00", "post-01", "post-02", "post-03", "post-04", "post-05"}, result)
}
