package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateFileContentSchema(conn *sqlx.DB) error {
	for _, name := range []string{"media_contents", "media_content_immutable", "file_content_versions", "file_content_versions_content", "file_content_versions_root", "file_content_version_valid", "file_content_version_immutable", "file_content_version_identity", "file_generation_forward", "file_generation_location", "file_generation_folder", "file_generation_fingerprint_insert", "file_generation_fingerprint_delete", "file_generation_fingerprint_update"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for _, field := range [][2]string{{"files", "generation"}, {"file_content_versions", "root_revision"}} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name=?)", field[0], field[1]); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s.%s", field[0], field[1])
		}
	}
	return nil
}
