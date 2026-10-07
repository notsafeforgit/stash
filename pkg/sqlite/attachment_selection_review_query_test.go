package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestAttachmentSelectionReviewPagesUniqueListsThroughScopedIndexes(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE source_post_identities(post_uuid TEXT PRIMARY KEY,canonical_uuid TEXT) WITHOUT ROWID;
 CREATE INDEX source_post_identities_canonical ON source_post_identities(canonical_uuid,post_uuid);
 INSERT INTO source_post_identities VALUES('post','post'),('original','post'),('unrelated','unrelated');
 CREATE TABLE source_attachment_manifests(uuid TEXT PRIMARY KEY,post_uuid TEXT,UNIQUE(post_uuid,uuid));
 CREATE TABLE source_capture_attachment_manifests(capture_uuid TEXT PRIMARY KEY,manifest_uuid TEXT) WITHOUT ROWID;
 CREATE INDEX source_capture_attachment_manifests_manifest ON source_capture_attachment_manifests(manifest_uuid,capture_uuid);
 INSERT INTO source_attachment_manifests VALUES('first','post'),('second','original'),('third','unrelated');
 INSERT INTO source_capture_attachment_manifests VALUES('capture1','first'),('capture2','first'),('capture3','second'),('capture4','third');`)
	require.NoError(t, err)
	var rows []struct {
		UUID        string `db:"uuid"`
		PostUUID    string `db:"post_uuid"`
		CaptureUUID string `db:"capture_uuid"`
	}
	require.NoError(t, db.Select(&rows, selectionReviewManifestsQuery, "", 1, "post", 1))
	require.Len(t, rows, 1)
	require.Equal(t, "first", rows[0].UUID)
	require.Equal(t, "capture1", rows[0].CaptureUUID)
	require.NoError(t, db.Select(&rows, selectionReviewManifestsQuery, rows[0].UUID, 1, "original", 1))
	require.Len(t, rows, 1)
	require.Equal(t, "second", rows[0].UUID)
	require.Equal(t, "original", rows[0].PostUUID)
	require.NoError(t, db.Select(&rows, selectionReviewManifestsQuery, "second", 1, "post", 1))
	require.Empty(t, rows)
	var plans []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+selectionReviewManifestsQuery, "", 25, "post", 25))
	text := fmt.Sprint(plans)
	require.Contains(t, text, "(post_uuid=? AND uuid>?)")
	require.Contains(t, text, "USING COVERING INDEX source_capture_attachment_manifests_manifest (manifest_uuid=?)")
	require.Contains(t, text, "source_post_identities_canonical (canonical_uuid=?)")
	require.NotContains(t, text, "SCAN source_attachment_manifests")
	require.NotContains(t, text, "SCAN source_capture_attachment_manifests")
}
