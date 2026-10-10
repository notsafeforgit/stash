package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateCatalogIdentitySchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"catalog_identity_imports", "catalog_identity_import_records", "catalog_identity_import_page", "catalog_identity_import_review", "catalog_identity_import_immutable", "catalog_identity_record_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_identity_imports i WHERE
 i.uuid IS NOT json_extract(i.plan,'$.uuid') OR i.source_uuid IS NOT json_extract(i.plan,'$.source_uuid')
 OR i.namespace IS NOT json_extract(i.plan,'$.namespace') OR i.input_sha256 IS NOT json_extract(i.plan,'$.input_sha256')
 OR i.plan_sha256 IS NOT json_extract(i.plan,'$.plan_sha256') OR i.record_count IS NOT json_extract(i.plan,'$.record_count')
 OR i.record_count IS NOT json_array_length(i.plan,'$.records')
 OR i.record_count!=(SELECT count(*) FROM catalog_identity_import_records r WHERE r.import_uuid=i.uuid)
 OR i.record_count!=(SELECT coalesce(sum(value),0) FROM json_each(i.plan,'$.inventory.retained_tables'))
 OR EXISTS(SELECT 1 FROM json_each(i.plan,'$.inventory.retained_tables') t WHERE t.value!=(SELECT count(*) FROM catalog_identity_import_records r WHERE r.import_uuid=i.uuid AND r.source_table=t.key)))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog identity import evidence")
	}
	return nil
}
