package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateCatalogRegistrySchema(conn *sqlx.DB) error {
	for _, name := range []string{"catalog_registry_imports", "catalog_registry_import_records", "catalog_registry_record_page", "catalog_registry_record_review", "catalog_registry_import_immutable", "catalog_registry_record_immutable", "catalog_account_mappings", "catalog_collection_mappings", "catalog_account_mapping_immutable", "catalog_collection_mapping_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM catalog_registry_imports i JOIN catalog_identity_imports p ON p.uuid=i.identity_import_uuid WHERE
 i.uuid IS NOT json_extract(i.plan,'$.uuid') OR i.source_uuid IS NOT json_extract(i.plan,'$.source_uuid')
 OR i.source_uuid!=p.source_uuid OR i.identity_import_uuid IS NOT json_extract(i.plan,'$.identity_import_uuid')
 OR i.input_sha256 IS NOT json_extract(i.plan,'$.input_sha256') OR i.plan_sha256 IS NOT json_extract(i.plan,'$.plan_sha256')
 OR i.record_count IS NOT json_extract(i.plan,'$.record_count') OR i.record_count IS NOT json_array_length(i.plan,'$.records')
 OR i.record_count!=(SELECT count(*) FROM catalog_registry_import_records r WHERE r.import_uuid=i.uuid)
 OR i.record_count!=(SELECT coalesce(sum(value),0) FROM json_each(i.plan,'$.inventory.retained_tables'))
 OR EXISTS(SELECT 1 FROM json_each(i.plan,'$.inventory.retained_tables') t WHERE t.value!=(SELECT count(*) FROM catalog_registry_import_records r WHERE r.import_uuid=i.uuid AND r.source_table=t.key)))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog registry import evidence")
	}
	if err := conn.Get(&invalid, `SELECT
 EXISTS(SELECT 1 FROM catalog_account_mappings m JOIN catalog_registry_imports i ON i.uuid=m.import_uuid WHERE m.source_uuid!=i.source_uuid)
 OR EXISTS(SELECT 1 FROM catalog_collection_mappings m JOIN catalog_registry_imports i ON i.uuid=m.import_uuid WHERE m.source_uuid!=i.source_uuid)
 OR EXISTS(SELECT 1 FROM catalog_registry_imports i,json_each(i.plan,'$.account_keys') k
 WHERE json_extract(k.value,'$.action')='mapped' AND NOT EXISTS(SELECT 1 FROM catalog_account_mappings m
 WHERE m.source_uuid=i.source_uuid AND m.account_key=json_extract(k.value,'$.account_key') AND m.import_uuid=i.uuid AND m.account_uuid=json_extract(k.value,'$.account_uuid')))
 OR EXISTS(SELECT 1 FROM catalog_registry_imports i,json_each(i.plan,'$.collections') c
 WHERE json_extract(c.value,'$.action')!='review' AND NOT EXISTS(SELECT 1 FROM catalog_collection_mappings m
 WHERE m.source_uuid=i.source_uuid AND m.catalog_id=json_extract(c.value,'$.catalog_id') AND m.import_uuid=i.uuid AND m.collection_uuid=json_extract(c.value,'$.collection_uuid')))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete catalog registry mappings")
	}
	return nil
}
