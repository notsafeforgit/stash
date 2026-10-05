package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateMetadataNameSchema(conn *sqlx.DB) error {
	for _, index := range []struct{ name, table, column, owner string }{
		{"metadata_studio_names", "studios", "name", "id"},
		{"metadata_studio_aliases", "studio_aliases", "alias", "studio_id"},
		{"metadata_tag_names", "tags", "name", "id"},
		{"metadata_tag_aliases", "tag_aliases", "alias", "tag_id"},
		{"metadata_group_names", "groups", "name", "id"},
	} {
		var valid bool
		if err := conn.Get(&valid, `SELECT EXISTS(SELECT 1 FROM sqlite_schema s
 JOIN pragma_index_list(?) l ON l.name=s.name
 WHERE s.type='index' AND s.name=? AND s.tbl_name=? AND l."unique"=0 AND l.partial=0)`, index.table, index.name, index.table); err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("native database schema is incomplete: missing or invalid %s", index.name)
		}
		var columns []struct {
			Name string `db:"name"`
			Coll string `db:"coll"`
			Desc int    `db:"desc"`
		}
		if err := conn.Select(&columns, `SELECT name,coll,"desc" FROM pragma_index_xinfo(?) WHERE "key"=1 ORDER BY seqno`, index.name); err != nil {
			return err
		}
		if len(columns) != 2 || columns[0].Name != index.column || columns[0].Coll != "NOCASE" || columns[0].Desc != 0 || columns[1].Name != index.owner || columns[1].Coll != "BINARY" || columns[1].Desc != 0 {
			return fmt.Errorf("native database schema has invalid name index %s", index.name)
		}
	}
	return nil
}
