package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

func init() {
	RegisterTransactionalMigration(NativeSchemaBaseline+121, func(ctx context.Context, tx *sqlx.Tx) error {
		return compactCatalogMemberships(ctx, tx, "")
	})
}

// compactCatalogMemberships runs inside the caller's transaction. It removes
// only untouched import groups and fully mapped inputs. Configured sources,
// unresolved imports and groups referenced by other domain data stay intact.
// source restricts subsequent import completion to that registry; the numbered
// migration passes an empty value to compact previously completed imports.
func compactCatalogMemberships(ctx context.Context, tx *sqlx.Tx, source string) error {
	for _, query := range []string{
		`CREATE TEMP TABLE membership_cleanup_groups(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
		`CREATE TEMP TABLE membership_cleanup_snapshots(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO membership_cleanup_groups
SELECT g.collection_uuid FROM catalog_membership_groups g
JOIN source_collections c ON c.uuid=g.collection_uuid
JOIN source_collection_revisions d ON d.collection_uuid=c.uuid AND d.revision=c.revision
WHERE (?='' OR g.source_uuid=?) AND c.revision=1 AND g.collection_revision=1
 AND d.state='disabled' AND d.origin='migration' AND d.target_url=''
 AND d.root_uuid IS NULL AND d.account_uuid IS NULL AND d.path_prefix=''
 AND d.label=g.source_label AND d.kind IN ('directory','subreddit')
 AND NOT EXISTS(SELECT 1 FROM source_collection_post_evidence e WHERE e.collection_uuid=c.uuid
  AND (e.origin!='migration' OR e.basis!='catalog-membership'
   OR NOT EXISTS(SELECT 1 FROM catalog_membership_records r WHERE r.membership_uuid=e.uuid AND r.collection_uuid=c.uuid AND r.post_uuid=e.post_uuid AND r.outcome='mapped')))`, source, source); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO membership_cleanup_snapshots
SELECT s.uuid FROM catalog_snapshots s JOIN catalog_membership_imports i ON i.snapshot_uuid=s.uuid
WHERE (?='' OR s.source_uuid=?) AND s.state='received' AND s.memberships_compacted=0
 AND i.state='mapped' AND i.review_records=0 AND i.processed_records=i.source_records`, source, source); err != nil {
		return err
	}
	var hasGroups bool
	if err := tx.GetContext(ctx, &hasGroups, "SELECT EXISTS(SELECT 1 FROM membership_cleanup_groups)"); err != nil {
		return err
	}
	if !hasGroups {
		// Fresh installs and empty inputs have no collection dependencies to
		// inspect. Configured groups excluded above must keep their receipts.
		if _, err := tx.ExecContext(ctx, `DELETE FROM membership_cleanup_snapshots WHERE uuid IN
(SELECT snapshot_uuid FROM catalog_membership_imports WHERE source_records>0)`); err != nil {
			return err
		}
		return finishMembershipCompaction(ctx, tx)
	}

	// Discover references rather than maintain an incomplete list as the native
	// schema grows. Unknown consumers exclude a group; they are never cascaded.
	var refs []struct {
		Table  string `db:"table_name"`
		Column string `db:"column_name"`
	}
	if err := tx.SelectContext(ctx, &refs, `SELECT DISTINCT s.name AS table_name,f."from" AS column_name
FROM sqlite_schema s JOIN pragma_foreign_key_list(s.name) f
WHERE s.type='table' AND f."table" IN ('source_collections','source_collection_revisions')
 AND f."to" IN ('uuid','collection_uuid')`); err != nil {
		return err
	}
	for _, ref := range refs {
		switch ref.Table {
		case "source_collections", "source_collection_revisions", "catalog_membership_groups", "catalog_membership_records", "source_collection_post_evidence", "source_collection_studios":
			continue
		}
		query := `DELETE FROM membership_cleanup_groups WHERE uuid IN (SELECT ` + quoteMembershipIdentifier(ref.Column) + ` FROM ` + quoteMembershipIdentifier(ref.Table) + `)`
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("checking import group use by %s: %w", ref.Table, err)
		}
	}
	// A snapshot and all its groups must be removable together. Groups may span
	// snapshots, so prune the connected set until no excluded reference remains.
	for {
		changed := int64(0)
		for _, query := range []string{
			`DELETE FROM membership_cleanup_groups WHERE uuid IN (SELECT collection_uuid FROM catalog_membership_records WHERE snapshot_uuid NOT IN (SELECT uuid FROM membership_cleanup_snapshots))`,
			`DELETE FROM membership_cleanup_snapshots WHERE uuid IN (SELECT snapshot_uuid FROM catalog_membership_records WHERE collection_uuid IS NULL OR collection_uuid NOT IN (SELECT uuid FROM membership_cleanup_groups))`,
		} {
			result, err := tx.ExecContext(ctx, query)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			changed += n
		}
		if changed == 0 {
			break
		}
	}
	// These are current domain values, with no import UUIDs, per-file receipts,
	// timestamps or synthetic source configuration retained alongside them.
	for _, query := range []string{
		`INSERT OR IGNORE INTO source_post_import_labels(post_uuid,kind,label,namespace)
SELECT e.post_uuid,d.kind,d.label,d.namespace FROM membership_cleanup_groups g
JOIN source_collection_post_evidence e ON e.collection_uuid=g.uuid
JOIN source_collection_revisions d ON d.collection_uuid=e.collection_uuid AND d.revision=e.collection_revision`,
		`INSERT OR IGNORE INTO source_post_studios(post_uuid,studio_id)
SELECT e.post_uuid,b.studio_id FROM membership_cleanup_groups g
JOIN source_collection_post_evidence e ON e.collection_uuid=g.uuid
JOIN source_collection_studios b ON b.collection_uuid=g.uuid WHERE b.studio_id IS NOT NULL`,
		`DELETE FROM catalog_membership_records WHERE snapshot_uuid IN (SELECT uuid FROM membership_cleanup_snapshots)`,
		`DELETE FROM source_collection_post_evidence WHERE collection_uuid IN (SELECT uuid FROM membership_cleanup_groups)`,
		`DELETE FROM source_collection_studios WHERE collection_uuid IN (SELECT uuid FROM membership_cleanup_groups)`,
		`DELETE FROM catalog_membership_groups WHERE collection_uuid IN (SELECT uuid FROM membership_cleanup_groups)`,
		// The base/revision FK is deferred. Both disappear in this transaction.
		`DELETE FROM source_collection_revisions WHERE collection_uuid IN (SELECT uuid FROM membership_cleanup_groups)`,
		`DELETE FROM source_collections WHERE uuid IN (SELECT uuid FROM membership_cleanup_groups)`,
		`DELETE FROM catalog_snapshot_records WHERE source_table='memberships' AND snapshot_uuid IN (SELECT uuid FROM membership_cleanup_snapshots)`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("compacting catalog memberships: %w", err)
		}
	}
	return finishMembershipCompaction(ctx, tx)
}

func finishMembershipCompaction(ctx context.Context, tx *sqlx.Tx) error {
	for _, query := range []string{
		`UPDATE catalog_snapshots SET memberships_compacted=1,
last_table=CASE WHEN last_table='memberships' THEN '' ELSE last_table END,
last_key=CASE WHEN last_table='memberships' THEN '[]' ELSE last_key END
WHERE uuid IN (SELECT uuid FROM membership_cleanup_snapshots)`,
		`DROP TABLE temp.membership_cleanup_snapshots`,
		`DROP TABLE temp.membership_cleanup_groups`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("compacting catalog memberships: %w", err)
		}
	}
	return nil
}

func quoteMembershipIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
