package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateFilePathSchema(conn *sqlx.DB) error {
	for _, name := range []string{"file_path_fences", "file_path_fences_folded", "file_path_fence_forward", "file_path_removed", "file_path_moved", "file_path_folder_moved"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	return nil
}
