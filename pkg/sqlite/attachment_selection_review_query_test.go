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
	_, err = db.Exec(`CREATE TABLE source_attachment_manifests(uuid TEXT PRIMARY KEY,post_uuid TEXT,UNIQUE(post_uuid,uuid));
 CREATE TABLE source_capture_attachment_manifests(capture_uuid TEXT PRIMARY KEY,manifest_uuid TEXT) WITHOUT ROWID;
 CREATE INDEX source_capture_attachment_manifests_manifest ON source_capture_attachment_manifests(manifest_uuid,capture_uuid);
 INSERT INTO source_attachment_manifests VALUES('first','post'),('second','post'),('third','unrelated');
 INSERT INTO source_capture_attachment_manifests VALUES('capture1','first'),('capture2','first'),('capture3','second'),('capture4','third');`)
	require.NoError(t, err)
	var rows []struct {
		UUID        string `db:"uuid"`
		PostUUID    string `db:"post_uuid"`
		CaptureUUID string `db:"capture_uuid"`
	}
	require.NoError(t, db.Select(&rows, selectionReviewManifestsQuery, "post", "", 1))
	require.Len(t, rows, 1)
	require.Equal(t, "first", rows[0].UUID)
	require.Equal(t, "capture1", rows[0].CaptureUUID)
	require.NoError(t, db.Select(&rows, selectionReviewManifestsQuery, "post", rows[0].UUID, 1))
	require.Len(t, rows, 1)
	require.Equal(t, "second", rows[0].UUID)
	var plans []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+selectionReviewManifestsQuery, "post", "", 25))
	text := fmt.Sprint(plans)
	require.Contains(t, text, "(post_uuid=? AND uuid>?)")
	require.Contains(t, text, "USING COVERING INDEX source_capture_attachment_manifests_manifest (manifest_uuid=?)")
	require.NotContains(t, text, "SCAN ")
	require.NotContains(t, text, "TEMP B-TREE")
}
