package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestGalleryMediaQueryUsesMembershipAndMaterializedOrderIndexes(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE archive_entities(uuid TEXT PRIMARY KEY,kind TEXT,image_id INTEGER,scene_id INTEGER);
CREATE INDEX archive_entities_image ON archive_entities(image_id);
CREATE INDEX archive_entities_scene ON archive_entities(scene_id);
CREATE TABLE images(id INTEGER PRIMARY KEY,title TEXT);
CREATE TABLE scenes(id INTEGER PRIMARY KEY,title TEXT);
CREATE TABLE galleries_images(gallery_id INTEGER,image_id INTEGER,PRIMARY KEY(gallery_id,image_id));
CREATE TABLE scenes_galleries(gallery_id INTEGER,scene_id INTEGER,PRIMARY KEY(gallery_id,scene_id));
CREATE TABLE images_files(image_id INTEGER,file_id INTEGER,"primary" INTEGER);
CREATE INDEX image_primary ON images_files(image_id) WHERE "primary"=1;
CREATE TABLE scenes_files(scene_id INTEGER,file_id INTEGER,"primary" INTEGER);
CREATE INDEX scene_primary ON scenes_files(scene_id) WHERE "primary"=1;
CREATE TABLE files(id INTEGER PRIMARY KEY,parent_folder_id INTEGER,basename TEXT);
CREATE TABLE folders(id INTEGER PRIMARY KEY,path TEXT);`)
	require.NoError(t, err)
	var plans []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+galleryLibraryMediaQuery, "[]", 1, 1, 60, 0))
	plan := fmt.Sprint(plans)
	require.Contains(t, plan, "SEARCH gi USING COVERING INDEX")
	require.Contains(t, plan, "SEARCH sg USING COVERING INDEX")
	require.Contains(t, plan, "SEARCH o USING AUTOMATIC COVERING INDEX (uuid=?)")
}
