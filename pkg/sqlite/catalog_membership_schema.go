package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateCatalogMembershipSchema(conn *sqlx.DB) error {
	for _, name := range []string{
		"source_collection_post_evidence", "source_collection_post_evidence_post", "source_collection_post_evidence_collection",
		"source_collection_post_evidence_immutable", "source_collection_post_evidence_active_post", "source_collection_post_evidence_revision",
		"catalog_membership_groups", "catalog_membership_group_immutable", "catalog_membership_imports", "catalog_membership_import_guard",
		"catalog_membership_records", "catalog_membership_review", "catalog_membership_record_immutable",
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var typed bool
	if err := conn.Get(&typed, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('source_collection_post_evidence') WHERE name='observed_at' AND upper(type)='DATETIME')"); err != nil {
		return err
	}
	if !typed {
		return errors.New("native database schema is incomplete: invalid source_collection_post_evidence.observed_at")
	}
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_collection_post_evidence e
 LEFT JOIN source_posts p ON p.uuid=e.post_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=e.collection_uuid AND c.revision=e.collection_revision
 WHERE p.uuid IS NULL OR c.collection_uuid IS NULL)
 OR EXISTS(SELECT 1 FROM catalog_membership_groups g
 LEFT JOIN catalog_snapshots s ON s.uuid=g.first_snapshot_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=g.collection_uuid AND c.revision=g.collection_revision
 WHERE s.source_uuid IS NOT g.source_uuid OR c.collection_uuid IS NULL
 OR c.label IS NOT g.source_label OR c.state IS NOT 'disabled' OR c.origin IS NOT 'migration'
 OR c.target_url IS NOT '' OR c.account_uuid IS NOT NULL OR c.root_uuid IS NOT NULL OR c.path_prefix IS NOT '')
 OR EXISTS(SELECT 1 FROM catalog_membership_imports i
 LEFT JOIN catalog_snapshots s ON s.uuid=i.snapshot_uuid LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=i.snapshot_uuid
 WHERE s.state IS NOT 'received' OR i.manifest_sha256 IS NOT s.manifest_sha256 OR i.policy!='catalog-membership-v1'
 OR e.state IS NULL OR e.state='running' OR e.manifest_sha256 IS NOT i.manifest_sha256
 OR i.source_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table='memberships')
 OR i.processed_records!=(SELECT count(*) FROM catalog_membership_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM catalog_membership_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM catalog_membership_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(r.ordinal),0) FROM catalog_membership_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM catalog_snapshot_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.source_table='memberships' AND r.ordinal<=i.last_ordinal)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0))
 OR EXISTS(SELECT 1 FROM catalog_membership_records r
 LEFT JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN catalog_snapshots s ON s.uuid=r.snapshot_uuid
 LEFT JOIN catalog_membership_groups g ON g.source_uuid=s.source_uuid AND g.source_key=json_extract(e.data,'$.values.collection_key')
 LEFT JOIN source_collection_post_evidence m ON m.uuid=r.membership_uuid
 WHERE e.source_table IS NOT 'memberships'
 OR (r.collection_uuid IS NOT NULL AND r.collection_uuid IS NOT g.collection_uuid)
 OR (r.membership_uuid IS NOT NULL AND (m.post_uuid IS NOT r.post_uuid OR m.collection_uuid IS NOT r.collection_uuid
  OR m.collection_revision IS NOT g.collection_revision OR m.origin IS NOT 'migration' OR m.basis IS NOT 'catalog-membership'
  OR g.source_kind IS NOT json_extract(e.data,'$.values.kind') OR g.source_label IS NOT json_extract(e.data,'$.values.label')
  OR json_extract(m.details,'$.snapshot_uuid') IS NOT r.snapshot_uuid OR json_extract(m.details,'$.source_ordinal') IS NOT r.ordinal
  OR json_extract(m.details,'$.source_sha256') IS NOT e.data_sha256)))`)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid catalog membership scope or progress")
	}
	rows, err := conn.Query(`SELECT g.source_key,g.source_kind,g.source_label,c.kind,c.namespace FROM catalog_membership_groups g
 JOIN source_collection_revisions c ON c.collection_uuid=g.collection_uuid AND c.revision=g.collection_revision`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, sourceKind, label, kind, namespace string
		if err := rows.Scan(&key, &sourceKind, &label, &kind, &namespace); err != nil {
			return err
		}
		definition, reason := catalogMembershipDefinition(key, sourceKind, label)
		if reason != "" || definition.Kind != kind || definition.Namespace != namespace {
			return errors.New("native database has invalid catalog membership collection definitions")
		}
	}
	return rows.Err()
}
