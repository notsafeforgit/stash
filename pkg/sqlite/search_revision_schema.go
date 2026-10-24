package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateSearchRevisionSchema(conn *sqlx.DB) error {
	objects := []string{"library_search_state", "library_search_state_retained"}
	for _, table := range []string{"scenes", "images", "scenes_files", "images_files", "files_fingerprints", "scene_markers"} {
		for _, event := range []string{"insert", "update", "delete"} {
			objects = append(objects, "library_search_"+table+"_"+event)
		}
	}
	objects = append(objects, "library_search_files_update", "library_search_files_delete", "library_search_folders_update")
	for _, name := range objects {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var valid bool
	if err := conn.Get(&valid, "SELECT count(*)=1 AND coalesce(sum(id=1 AND length(token)=32 AND token NOT GLOB '*[^0-9a-f]*'),0)=1 FROM library_search_state"); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("native database has invalid search revision state")
	}
	return nil
}
